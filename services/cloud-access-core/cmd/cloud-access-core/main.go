package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	consumers "github.com/hayhandsome/msbd/services/cloud-access-core/internal/consumers"
	httpapi "github.com/hayhandsome/msbd/services/cloud-access-core/internal/http"
	configmodule "github.com/hayhandsome/msbd/services/cloud-access-core/internal/modules/configcenter"
	gatewaymodule "github.com/hayhandsome/msbd/services/cloud-access-core/internal/modules/gateway"
	iammodule "github.com/hayhandsome/msbd/services/cloud-access-core/internal/modules/iam"
	policymodule "github.com/hayhandsome/msbd/services/cloud-access-core/internal/modules/policy"
	"github.com/hayhandsome/msbd/services/cloud-access-core/internal/ws"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/configcenter"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/gateway"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/iam"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/policy"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/rate"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/telemetry"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := bootstrap.LoadConfigFromEnv()
	if err != nil {
		panic(err)
	}

	logger := bootstrap.NewLogger(cfg.Logging).With("service", "cloud-access-core")

	app := bootstrap.NewApp(cfg, logger)
	metrics := telemetry.NewCollector()
	app.Router().Handle("/metrics", metrics.Handler())

	db, sqlClose, err := connectDatabase(cfg.Database)
	if err != nil {
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer sqlClose()
	repo := gormdb.NewRepository(db)
	if err := gormdb.EnsureSeedData(ctx, repo); err != nil {
		logger.Error("failed to ensure seed data", "error", err)
		os.Exit(1)
	}

	redisClient, err := connectRedis(ctx, cfg.Redis)
	if err != nil {
		logger.Error("redis connection failed", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	signer, err := crypto.NewSigner(crypto.Options{
		Issuer:           cfg.Auth.Issuer,
		KeyID:            cfg.Auth.Signing.KeyID,
		PrivateKeyBase64: cfg.Auth.Signing.PrivateKeyBase64,
		PublicKeyBase64:  cfg.Auth.Signing.PublicKeyBase64,
	})
	if err != nil {
		logger.Error("failed to init signer", "error", err)
		os.Exit(1)
	}

	eventPublisher := events.NewCompositePublisher(
		events.NewRedisStreamPublisher(redisClient, cfg.Telemetry.StreamName(), cfg.Telemetry.MaxStreamLength(), logger),
		events.NewKafkaStubPublisher(cfg.Telemetry.KafkaTopicName(), logger),
	)
	buffer, err := consumers.NewBadgerBuffer(consumers.BadgerBufferConfig{
		Path:          cfg.Telemetry.BufferDir(),
		FlushInterval: cfg.Telemetry.FlushInterval(),
		MaxBatch:      256,
		OnChange:      metrics.SetBufferedEvents,
	}, logger)
	if err != nil {
		logger.Error("failed to initialize telemetry buffer", "error", err)
		os.Exit(1)
	}
	defer buffer.Close()
	if err := buffer.Start(ctx, eventPublisher); err != nil {
		logger.Error("failed to start telemetry buffer", "error", err)
		os.Exit(1)
	}

	sessionStore := iam.NewRedisSessionStore(redisClient, cfg.Auth.SessionPrefix)
	auditor := telemetry.NewAuditRecorder(repo, eventPublisher, buffer, metrics, logger, telemetry.AuditRecorderConfig{Topic: "audit.events"})
	iamService := iam.NewService(repo, sessionStore, signer, auditor, logger, iam.ServiceConfig{
		AccessTokenTTL:  cfg.Auth.AccessTokenTTL(),
		DeviceTokenTTL:  cfg.Auth.DeviceTokenTTL(),
		RefreshTokenTTL: cfg.Auth.RefreshTokenTTL(),
		WebAudience:     cfg.Auth.WebAudience,
		DeviceAudience:  cfg.Auth.DeviceAudience,
	})
	deviceService := iam.NewDeviceService(
		repo,
		iam.NewRedisPairingStore(redisClient, cfg.Device.PairingStorePrefix),
		iam.NewRedisDeviceNotifier(redisClient, cfg.Device.RevocationChannel),
		events.NewRedisHeartbeatPublisher(redisClient, cfg.Device.HeartbeatStream, logger),
		auditor,
		logger,
		iam.DeviceServiceConfig{
			PairingCodeTTL: cfg.Device.PairingCodeTTL(),
			CodeLength:     cfg.Device.CodeLength(),
			DefaultProfile: cfg.Device.DefaultProfileName,
			EncryptionKey:  decodeKey(cfg.Device.ProfileKeyBase64),
		},
	)
	heartbeatConsumer := events.NewHeartbeatConsumer(redisClient, repo, events.HeartbeatConsumerConfig{
		Stream:   cfg.Device.HeartbeatStream,
		Group:    cfg.Device.HeartbeatGroup,
		Consumer: fmt.Sprintf("iam-consumer-%d", time.Now().UnixNano()),
	}, logger)
	policyBucket := rate.NewRedisTokenBucket(redisClient, cfg.Policy.BucketPrefix)
	policyPublisher := events.NewRedisPolicyPublisher(redisClient, cfg.Policy.UpdateChannel, logger)
	policyService, err := policy.NewService(repo, policyBucket, policyPublisher, logger, policy.ServiceConfig{
		QuotaWindow: cfg.Policy.QuotaWindow(),
	})
	if err != nil {
		logger.Error("failed to init policy service", "error", err)
		os.Exit(1)
	}
	iamModule := iammodule.New(iamService, deviceService, heartbeatConsumer, logger, iammodule.Config{GRPCAddress: cfg.Server.GRPCAddress})
	policyModule := policymodule.New(policyService, logger, policymodule.Config{GRPCAddress: cfg.Policy.GRPCAddress})

	gatewayLimiter := rate.NewRedisTokenBucket(redisClient, cfg.Gateway.RateLimitPrefix)
	gatewayServer := gateway.NewServer(iamService, policyService, gatewayLimiter, logger, gateway.Config{
		Enabled:             cfg.Gateway.Enabled,
		RoutesPath:          cfg.Gateway.RoutesPath,
		AdminToken:          cfg.Gateway.AdminToken,
		DefaultCapacity:     cfg.Gateway.DefaultCapacity,
		DefaultRefillPerSec: cfg.Gateway.DefaultRefillPerSec,
		DefaultWindow:       time.Duration(cfg.Gateway.DefaultWindowSeconds) * time.Second,
		CertBundlePath:      cfg.Gateway.CertBundlePath,
	})
	if err := gatewayServer.LoadRoutes(); err != nil {
		logger.Warn("failed to load gateway routes", "error", err)
	}

	configPublisher := events.NewRedisConfigPublisher(redisClient, cfg.Config.UpdateChannelPrefix, logger)
	configService := configcenter.NewService(repo, configPublisher, auditor, logger, configcenter.ServiceConfig{
		DefaultProfile: cfg.Device.DefaultProfileName,
		EncryptionKey:  decodeKey(cfg.Device.ProfileKeyBase64),
		SigningKey:     decodeKey(cfg.Config.SigningKeyBase64),
	})
	configSubscriber := events.NewRedisConfigSubscriber(redisClient, cfg.Config.UpdateChannelPrefix, logger)
	configHandler := httpapi.NewConfigHandler(iamService, repo, configService, logger)
	configStream := ws.NewConfigStreamHandler(iamService, configSubscriber, logger)
	configModule := configmodule.New(configHandler, configStream)

	modules := []bootstrap.Module{
		bootstrap.NewHealthModule(),
		iamModule,
		policyModule,
		configModule,
	}
	if gatewayServer.Enabled() {
		modules = append(modules, gatewaymodule.New(gatewayServer))
	}

	if err := app.RegisterModules(modules...); err != nil {
		logger.Error("failed to register modules", "error", err)
		os.Exit(1)
	}

	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("service exited with error", "error", err)
		os.Exit(1)
	}

	logger.Info("service stopped")
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
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime())
	return db, func() { _ = sqlDB.Close() }, nil
}

func connectRedis(ctx context.Context, cfg bootstrap.RedisConfig) (*redis.Client, error) {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return client, nil
}

func decodeKey(encoded string) []byte {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	return data
}
