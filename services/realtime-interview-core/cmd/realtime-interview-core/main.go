package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	cloudEvents "github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/rate"
	internalEvents "github.com/hayhandsome/msbd/services/realtime-interview-core/internal/events"
	controlgrpc "github.com/hayhandsome/msbd/services/realtime-interview-core/internal/grpc/control"
	ingressmodule "github.com/hayhandsome/msbd/services/realtime-interview-core/internal/modules/ingress"
	sessionmodule "github.com/hayhandsome/msbd/services/realtime-interview-core/internal/modules/sessions"
	usagemodule "github.com/hayhandsome/msbd/services/realtime-interview-core/internal/modules/usage"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/internal/ws"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/authn"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/iamproxy"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm"
	_ "github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm/deepseek"
	_ "github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm/glm"
	_ "github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm/openai"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/metrics"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/orchestrator"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/providers/aliyun"
	sessionstore "github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/sessions"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/speechgateway"
	usagepkg "github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/usage"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := bootstrap.LoadConfigFromEnv()
	if err != nil {
		panic(err)
	}

	logger := bootstrap.NewLogger(cfg.Logging).With("service", "realtime-interview-core")

	app := bootstrap.NewApp(cfg, logger)
	collector := metrics.NewCollector(nil)
	app.Router().Handle("/metrics", collector.Handler())

	db, closeDB, err := connectDatabase(cfg.Database)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer closeDB()

	redisClient, err := connectRedis(ctx, cfg.Redis)
	if err != nil {
		logger.Error("redis connection failed", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	iamClient, err := iamproxy.NewClient(ctx, cfg.IAMClient, logger)
	if err != nil {
		logger.Error("iam client init failed", "error", err)
		os.Exit(1)
	}
	defer iamClient.Close()

	guard := authn.NewGuard(iamClient, logger)
	if strings.EqualFold(cfg.Env, "development") {
		app.Router().Handle("/debug/auth/whoami", guard.HTTPMiddleware(authn.HTTPOptions{
			TokenFormat:    crypto.TokenKindJWT,
			AllowAnonymous: false,
		})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := authn.ClaimsFromContext(r.Context())
			w.Header().Set("Content-Type", "application/json")
			if !ok {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "anonymous"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"subject":    claims.Subject,
				"tenant_id":  claims.TenantID,
				"session_id": claims.SessionID,
				"email":      claims.Email,
				"roles":      claims.Roles,
			})
		})))
	}
	streamMaxLen := cfg.Telemetry.MaxStreamLength()
	sessionRepo := sessionstore.NewRepository(db)

	var (
		usageService  *usagepkg.Service
		usageExporter *usagepkg.Exporter
	)
	if cfg.Usage.Enabled {
		usagePublisher := cloudEvents.NewRedisStreamPublisher(redisClient, cfg.Streams.UsageStreamName(), streamMaxLen, logger)
		var usageBuffer *usagepkg.BadgerBuffer
		bufferCfg := usagepkg.BadgerBufferConfig{Path: cfg.Usage.BufferPath, FlushInterval: cfg.Usage.FlushInterval(), MaxBatch: cfg.Usage.BatchSize, OnChange: collector.SetUsageBuffered}
		if buf, err := usagepkg.NewBadgerBuffer(bufferCfg, logger); err != nil {
			logger.Warn("usage buffer init failed", "error", err)
		} else {
			usageBuffer = buf
		}
		usageExporter = usagepkg.NewExporter(sessionRepo, usagePublisher, usageBuffer, cfg.Streams.UsageStreamName(), collector, logger)
		usageService = usagepkg.NewService(cfg.Usage, usageExporter, collector, logger)
	}

	bucket := rate.NewRedisTokenBucket(redisClient, "realtime-interview-core:speech")
	speechSvc := speechgateway.NewService(cfg.Speech, cfg.Providers.Default, redisClient, bucket, collector, logger)
	aliyunAdapter := aliyun.NewAdapter(cfg.Providers.Aliyun, nil, logger)
	speechSvc.RegisterProvider("aliyun", aliyunAdapter)
	runtimeStore := speechgateway.NewConfigStore(redisClient, cfg.Speech.RuntimeKey(), cfg.Speech.RuntimeChannel(), logger)
	speechgateway.InitializeRuntimeConfig(ctx, runtimeStore, cfg.Providers, speechSvc, speechgateway.ProviderAdapters{Aliyun: aliyunAdapter}, logger)

	sessionPublisher := cloudEvents.NewRedisStreamPublisher(redisClient, cfg.Streams.SessionEventsName(), streamMaxLen, logger)
	sessionService := sessionstore.NewService(sessionRepo, sessionPublisher, logger)
	controlServer := controlgrpc.NewServer(sessionService, logger)

	transcriptPublisher := cloudEvents.NewRedisStreamPublisher(redisClient, cfg.Orchestrator.QueueStream, streamMaxLen, logger)
	orchestratorPublisher := cloudEvents.NewRedisStreamPublisher(redisClient, cfg.Streams.OrchestratorOutputsName(), streamMaxLen, logger)

	providerSettings := cfg.Orchestrator.ProviderSettings()
	llmClient, err := llm.NewClient(cfg.Orchestrator.Provider, llm.ProviderConfig{
		Name:           cfg.Orchestrator.Provider,
		APIKey:         providerSettings.APIKey,
		Base:           providerSettings.BaseURL,
		Model:          providerSettings.Model,
		TimeoutSeconds: providerSettings.TimeoutSeconds,
		Extra:          providerSettings.Extra,
	}, logger)
	if err != nil {
		logger.Error("llm client init failed", "error", err)
		os.Exit(1)
	}
	toolchain, err := orchestrator.NewPromptToolchain(cfg.Orchestrator, llmClient, logger)
	if err != nil {
		logger.Error("toolchain init failed", "error", err)
		os.Exit(1)
	}
	orchestratorSvc, err := orchestrator.NewService(cfg.Orchestrator, cfg.Streams.OrchestratorOutputsName(), toolchain, orchestratorPublisher, redisClient, usageService, logger)
	if err != nil {
		logger.Error("orchestrator service init failed", "error", err)
		os.Exit(1)
	}
	orchModule := internalEvents.NewOrchestratorConsumer(redisClient, cfg.Orchestrator.QueueStream, orchestratorSvc, logger)

	streamBindings := []ws.StreamBinding{
		{Name: cfg.Orchestrator.QueueStream, Kind: "transcript.final"},
		{Name: cfg.Streams.OrchestratorOutputsName(), Kind: "orchestrator.output"},
		{Name: cfg.Streams.SessionEventsName(), Kind: "session.event"},
	}
	if ctrl := cfg.Streams.ControlStreamName(); strings.TrimSpace(ctrl) != "" {
		streamBindings = append(streamBindings, ws.StreamBinding{Name: ctrl, Kind: "control"})
	}
	wsHub := ws.NewHub(redisClient, ws.HubConfig{Streams: streamBindings}, collector, logger)

	modules := []bootstrap.Module{
		bootstrap.NewHealthModule(),
	}
	if usageModule := usagemodule.New(usageService, usageExporter, logger); usageModule != nil {
		modules = append(modules, usageModule)
	}
	if orchModule != nil {
		modules = append(modules, orchModule)
	}
	if sessionHTTPModule := sessionmodule.New(sessionmodule.Config{APIPrefix: "/api/realtime", WebsocketPath: "/ws/realtime"}, guard, sessionService, speechSvc, runtimeStore, wsHub, logger); sessionHTTPModule != nil {
		modules = append(modules, sessionHTTPModule)
	}
	if ingressModule := ingressmodule.New(cfg.Server, guard, speechSvc, transcriptPublisher, usageService, controlServer, logger); ingressModule != nil {
		modules = append(modules, ingressModule)
	}

	if err := app.RegisterModules(modules...); err != nil {
		logger.Error("failed to register modules", "error", err)
		os.Exit(1)
	}

	logger.Info("service starting",
		"env", cfg.Env,
		"http_addr", cfg.Server.Address,
		"grpc_addr", cfg.Server.GRPCAddress,
		"iam_addr", cfg.IAMClient.Address,
		"db", maskDSN(cfg.Database.DSN))

	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("service exited with error", "error", err)
		os.Exit(1)
	}

	logger.Info("service stopped")

	_ = db
}

func connectDatabase(cfg bootstrap.DatabaseConfig) (*gorm.DB, func(), error) {
	if strings.TrimSpace(cfg.DSN) == "" {
		return nil, nil, fmt.Errorf("database dsn required")
	}
	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("sql db handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime())
	if err := sqlDB.Ping(); err != nil {
		return nil, nil, fmt.Errorf("ping database: %w", err)
	}
	return db, func() { _ = sqlDB.Close() }, nil
}

func connectRedis(ctx context.Context, cfg bootstrap.RedisConfig) (*redis.Client, error) {
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil, fmt.Errorf("redis addr required")
	}
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}

func maskDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	parts := strings.Split(dsn, "@")
	if len(parts) != 2 {
		return dsn
	}
	creds := strings.SplitN(parts[0], "//", 2)
	if len(creds) != 2 {
		return dsn
	}
	masked := creds[0] + "//***:***"
	return masked + "@" + parts[1]
}
