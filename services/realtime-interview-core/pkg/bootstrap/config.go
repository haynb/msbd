package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultConfigPath = "configs/config.yaml"
	envConfigPathKey  = "REALTIME_INTERVIEW_CONFIG"
)

// Config aggregates runtime configuration values shared across modules.
type Config struct {
	Env          string              `json:"env"`
	Server       ServerConfig        `json:"server"`
	Logging      LoggingConfig       `json:"logging"`
	Telemetry    TelemetryConfig     `json:"telemetry"`
	Database     DatabaseConfig      `json:"database"`
	Redis        RedisConfig         `json:"redis"`
	IAMClient    IAMClientConfig     `json:"iam_client"`
	Speech       SpeechGatewayConfig `json:"speech_gateway"`
	Providers    ProvidersConfig     `json:"providers"`
	Orchestrator OrchestratorConfig  `json:"orchestrator"`
	Usage        UsageExportConfig   `json:"usage_export"`
	Streams      StreamNamesConfig   `json:"streams"`
}

// ServerConfig describes HTTP server parameters.
type ServerConfig struct {
	Address              string `json:"address"`
	GRPCAddress          string `json:"grpc_address"`
	ShutdownGraceSeconds int    `json:"shutdown_grace_seconds"`
}

// LoggingConfig controls log output.
type LoggingConfig struct {
	Level  string `json:"level"`
	Pretty bool   `json:"pretty"`
}

// TelemetryConfig configures metrics endpoints.
type TelemetryConfig struct {
	PrometheusAddr       string `json:"prometheus_addr"`
	EventStream          string `json:"event_stream"`
	KafkaTopic           string `json:"kafka_topic"`
	BufferPath           string `json:"buffer_path"`
	FlushIntervalSeconds int    `json:"flush_interval_seconds"`
	StreamMaxLen         int64  `json:"stream_max_len"`
}

// StreamName returns the configured Redis stream for audit mirrors.
func (c TelemetryConfig) StreamName() string {
	if strings.TrimSpace(c.EventStream) == "" {
		return "realtime-interview-core:telemetry"
	}
	return c.EventStream
}

// KafkaTopicName returns the configured Kafka topic placeholder.
func (c TelemetryConfig) KafkaTopicName() string {
	if strings.TrimSpace(c.KafkaTopic) == "" {
		return "realtime-interview-core.telemetry"
	}
	return c.KafkaTopic
}

// BufferDir returns the directory for the local telemetry buffer.
func (c TelemetryConfig) BufferDir() string {
	if strings.TrimSpace(c.BufferPath) == "" {
		return filepath.Join("data", "telemetry-buffer")
	}
	return c.BufferPath
}

// FlushInterval returns the buffer flush cadence.
func (c TelemetryConfig) FlushInterval() time.Duration {
	if c.FlushIntervalSeconds <= 0 {
		return 5 * time.Second
	}
	return time.Duration(c.FlushIntervalSeconds) * time.Second
}

// MaxStreamLength ensures Redis stream trimming has a sane default.
func (c TelemetryConfig) MaxStreamLength() int64 {
	if c.StreamMaxLen <= 0 {
		return 1000
	}
	return c.StreamMaxLen
}

// DatabaseConfig controls GORM/PostgreSQL connectivity.
type DatabaseConfig struct {
	DSN                    string `json:"dsn"`
	MaxOpenConns           int    `json:"max_open_conns"`
	MaxIdleConns           int    `json:"max_idle_conns"`
	ConnMaxLifetimeSeconds int    `json:"conn_max_lifetime_seconds"`
}

// ConnMaxLifetime returns the configured connection lifetime or a safe default.
func (c DatabaseConfig) ConnMaxLifetime() time.Duration {
	if c.ConnMaxLifetimeSeconds <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(c.ConnMaxLifetimeSeconds) * time.Second
}

// RedisConfig configures the session/event cache connection.
type RedisConfig struct {
	Addr     string `json:"addr"`
	Password string `json:"password"`
	DB       int    `json:"db"`
}

// IAMClientConfig describes how we talk to cloud-access-core.
type IAMClientConfig struct {
	Address        string `json:"address"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Insecure       bool   `json:"insecure"`
}

// Timeout returns the outbound RPC timeout.
func (c IAMClientConfig) Timeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return 3 * time.Second
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// SpeechGatewayConfig tunes the ingest gateway.
type SpeechGatewayConfig struct {
	Enabled                    bool   `json:"enabled"`
	MaxChunkBytes              int    `json:"max_chunk_bytes"`
	AckDeadlineMillis          int    `json:"ack_deadline_millis"`
	SessionIdleTimeoutSeconds  int    `json:"session_idle_timeout_seconds"`
	BufferPrefix               string `json:"buffer_prefix"`
	BufferTTLSeconds           int    `json:"buffer_ttl_seconds"`
	CircuitBreakerFailures     int    `json:"circuit_breaker_failures"`
	CircuitBreakerResetSeconds int    `json:"circuit_breaker_reset_seconds"`
	RuntimeConfigKey           string `json:"runtime_config_key"`
	RuntimeUpdateChannel       string `json:"runtime_update_channel"`
}

// AckDeadline returns the ACK window for handshake responses.
func (c SpeechGatewayConfig) AckDeadline() time.Duration {
	if c.AckDeadlineMillis <= 0 {
		return 100 * time.Millisecond
	}
	return time.Duration(c.AckDeadlineMillis) * time.Millisecond
}

// SessionIdleTimeout returns the allowed idle time for a stream.
func (c SpeechGatewayConfig) SessionIdleTimeout() time.Duration {
	if c.SessionIdleTimeoutSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.SessionIdleTimeoutSeconds) * time.Second
}

// BufferTTL returns how long buffered chunks stay in Redis.
func (c SpeechGatewayConfig) BufferTTL() time.Duration {
	if c.BufferTTLSeconds <= 0 {
		return 10 * time.Second
	}
	return time.Duration(c.BufferTTLSeconds) * time.Second
}

// CircuitReset returns the cool-down duration for the breaker.
func (c SpeechGatewayConfig) CircuitReset() time.Duration {
	if c.CircuitBreakerResetSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.CircuitBreakerResetSeconds) * time.Second
}

// RuntimeKey returns the Redis key that stores provider overrides.
func (c SpeechGatewayConfig) RuntimeKey() string {
	key := strings.TrimSpace(c.RuntimeConfigKey)
	if key == "" {
		return "realtime-interview-core:speech-gateway:runtime"
	}
	return key
}

// RuntimeChannel returns the Redis Pub/Sub channel for runtime provider updates.
func (c SpeechGatewayConfig) RuntimeChannel() string {
	channel := strings.TrimSpace(c.RuntimeUpdateChannel)
	if channel == "" {
		return "realtime-interview-core:speech-gateway:updates"
	}
	return channel
}

// ProvidersConfig encapsulates speech providers.
type ProvidersConfig struct {
	Default string               `json:"default"`
	Aliyun  AliyunProviderConfig `json:"aliyun"`
}

// AliyunProviderConfig carries SDK knobs.
type AliyunProviderConfig struct {
	APIURL                         string `json:"api_url"`
	Domain                         string `json:"domain"`
	AppKey                         string `json:"app_key"`
	Token                          string `json:"token"`
	TokenURL                       string `json:"token_url"`
	Format                         string `json:"format"`
	SampleRate                     int    `json:"sample_rate"`
	EnableIntermediateResult       bool   `json:"enable_intermediate_result"`
	EnableInverseTextNormalization bool   `json:"enable_itn"`
	EnablePunctuation              bool   `json:"enable_punctuation"`
	EnableVoiceDetection           bool   `json:"enable_voice_detection"`
}

// OrchestratorConfig controls LLM orchestration.
type OrchestratorConfig struct {
	Provider            string                      `json:"provider"`
	ShortReplyTemplate  string                      `json:"short_reply_template"`
	DetailedTemplate    string                      `json:"detailed_analysis_template"`
	CoachHintTemplate   string                      `json:"coach_hint_template"`
	BudgetWindowSeconds int                         `json:"budget_window_seconds"`
	MaxTokensPerWindow  int                         `json:"max_tokens_per_window"`
	QueueStream         string                      `json:"queue_stream"`
	Providers           OrchestratorProvidersConfig `json:"providers"`
}

// OrchestratorProvidersConfig contains provider-specific credentials.
type OrchestratorProvidersConfig struct {
	OpenAI   LLMProviderSettings            `json:"openai"`
	DeepSeek LLMProviderSettings            `json:"deepseek"`
	GLM      LLMProviderSettings            `json:"glm"`
	Custom   map[string]LLMProviderSettings `json:"custom"`
}

// LLMProviderSettings describes an LLM endpoint + credentials.
type LLMProviderSettings struct {
	APIKey         string            `json:"api_key"`
	BaseURL        string            `json:"base_url"`
	Model          string            `json:"model"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	Extra          map[string]string `json:"extra"`
}

// BudgetWindow returns the enforcement window duration.
func (c OrchestratorConfig) BudgetWindow() time.Duration {
	if c.BudgetWindowSeconds <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(c.BudgetWindowSeconds) * time.Second
}

// ProviderSettings selects the configured provider settings.
func (c OrchestratorConfig) ProviderSettings() LLMProviderSettings {
	providers := c.Providers
	key := strings.ToLower(strings.TrimSpace(c.Provider))
	if key == "" {
		key = "openai"
	}
	switch key {
	case "deepseek":
		if providers.DeepSeek.APIKey != "" || providers.DeepSeek.BaseURL != "" || providers.DeepSeek.Model != "" {
			return providers.DeepSeek
		}
	case "glm", "glm46", "glm-4.6", "glm4", "glm-4":
		if providers.GLM.APIKey != "" || providers.GLM.BaseURL != "" || providers.GLM.Model != "" {
			return providers.GLM
		}
	default:
		if providers.Custom != nil {
			if cfg, ok := providers.Custom[key]; ok {
				return cfg
			}
		}
	}
	return providers.OpenAI
}

// UsageExportConfig defines Postgres/Redis writeback cadence.
type UsageExportConfig struct {
	Enabled              bool   `json:"enabled"`
	FlushIntervalSeconds int    `json:"flush_interval_seconds"`
	BatchSize            int    `json:"batch_size"`
	BufferPath           string `json:"buffer_path"`
}

// FlushInterval returns flush cadence for usage export.
func (c UsageExportConfig) FlushInterval() time.Duration {
	if c.FlushIntervalSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(c.FlushIntervalSeconds) * time.Second
}

// StreamNamesConfig holds redis stream identifiers.
type StreamNamesConfig struct {
	SessionEvents       string `json:"session_events"`
	SpeechMetrics       string `json:"speech_metrics"`
	OrchestratorOutputs string `json:"orchestrator_outputs"`
	Usage               string `json:"usage"`
	Control             string `json:"control"`
}

func (c StreamNamesConfig) SessionEventsName() string {
	if strings.TrimSpace(c.SessionEvents) == "" {
		return "realtime-interview-core:session.events"
	}
	return c.SessionEvents
}

func (c StreamNamesConfig) SpeechMetricsName() string {
	if strings.TrimSpace(c.SpeechMetrics) == "" {
		return "realtime-interview-core:speech.metrics"
	}
	return c.SpeechMetrics
}

func (c StreamNamesConfig) OrchestratorOutputsName() string {
	if strings.TrimSpace(c.OrchestratorOutputs) == "" {
		return "realtime-interview-core:orchestrator.outputs"
	}
	return c.OrchestratorOutputs
}

func (c StreamNamesConfig) UsageStreamName() string {
	if strings.TrimSpace(c.Usage) == "" {
		return "realtime-interview-core:usage"
	}
	return c.Usage
}

func (c StreamNamesConfig) ControlStreamName() string {
	if strings.TrimSpace(c.Control) == "" {
		return "realtime-interview-core:control"
	}
	return c.Control
}

// ShutdownTimeout returns the configured grace period or a safe default.
func (s ServerConfig) ShutdownTimeout() time.Duration {
	if s.ShutdownGraceSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(s.ShutdownGraceSeconds) * time.Second
}

// LoadConfigFromEnv loads configuration using the REALTIME_INTERVIEW_CONFIG env variable if present.
func LoadConfigFromEnv() (*Config, error) {
	return LoadConfig(os.Getenv(envConfigPathKey))
}

// LoadConfig reads configuration from the provided path and merges with defaults and env overrides.
func LoadConfig(path string) (*Config, error) {
	cfg := defaultConfig()

	if path == "" {
		path = searchFallbackConfig()
	}

	if path != "" {
		if err := mergeConfigFromFile(cfg, path); err != nil {
			return nil, err
		}
	}

	cfg.applyEnvOverrides()
	return cfg, nil
}

func mergeConfigFromFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config %q: %w", path, err)
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parse config %q: %w", path, err)
	}
	return nil
}

func searchFallbackConfig() string {
	candidates := []string{
		defaultConfigPath,
		filepath.Join("..", defaultConfigPath),
		filepath.Join("..", "..", defaultConfigPath),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func defaultConfig() *Config {
	return &Config{
		Env: "development",
		Server: ServerConfig{
			Address:              ":8084",
			GRPCAddress:          ":9084",
			ShutdownGraceSeconds: 20,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Pretty: true,
		},
		Telemetry: TelemetryConfig{
			PrometheusAddr:       ":9096",
			EventStream:          "realtime-interview-core:telemetry",
			KafkaTopic:           "realtime-interview-core.telemetry",
			BufferPath:           filepath.Join("data", "telemetry-buffer"),
			FlushIntervalSeconds: 5,
			StreamMaxLen:         2000,
		},
		Database: DatabaseConfig{
			DSN:                    "postgres://postgres:postgres@localhost:5432/realtime_interview?sslmode=disable",
			MaxOpenConns:           20,
			MaxIdleConns:           10,
			ConnMaxLifetimeSeconds: 1800,
		},
		Redis: RedisConfig{
			Addr: "127.0.0.1:6379",
			DB:   0,
		},
		IAMClient: IAMClientConfig{
			Address:        "127.0.0.1:9091",
			TimeoutSeconds: 3,
			Insecure:       true,
		},
		Speech: SpeechGatewayConfig{
			Enabled:                    true,
			MaxChunkBytes:              65536,
			AckDeadlineMillis:          100,
			SessionIdleTimeoutSeconds:  30,
			BufferPrefix:               "realtime-interview-core:chunks",
			BufferTTLSeconds:           12,
			CircuitBreakerFailures:     5,
			CircuitBreakerResetSeconds: 30,
		},
		Providers: ProvidersConfig{
			Default: "aliyun",
			Aliyun: AliyunProviderConfig{
				APIURL:                         "wss://nls-gateway-cn-shanghai.aliyuncs.com/ws/v1",
				Domain:                         "nls-meta.cn-shanghai.aliyuncs.com",
				Format:                         "pcm",
				SampleRate:                     16000,
				EnableIntermediateResult:       true,
				EnableInverseTextNormalization: true,
				EnablePunctuation:              true,
				EnableVoiceDetection:           true,
			},
		},
		Orchestrator: OrchestratorConfig{
			Provider:            "openai",
			ShortReplyTemplate:  "configs/prompts/short_reply.tmpl",
			DetailedTemplate:    "configs/prompts/detailed_analysis.tmpl",
			CoachHintTemplate:   "configs/prompts/coach_hint.tmpl",
			BudgetWindowSeconds: 300,
			MaxTokensPerWindow:  8000,
			QueueStream:         "realtime-interview-core:orchestrator.queue",
			Providers: OrchestratorProvidersConfig{
				OpenAI: LLMProviderSettings{
					BaseURL: "https://api.openai.com/v1/chat/completions",
					Model:   "gpt-4o-mini",
				},
				DeepSeek: LLMProviderSettings{
					BaseURL: "https://api.deepseek.com/chat/completions",
					Model:   "deepseek-chat",
				},
				GLM: LLMProviderSettings{
					BaseURL: "https://open.bigmodel.cn/api/paas/v4/chat/completions",
					Model:   "glm-4.5",
				},
				Custom: map[string]LLMProviderSettings{},
			},
		},
		Usage: UsageExportConfig{
			Enabled:              true,
			FlushIntervalSeconds: 15,
			BatchSize:            128,
			BufferPath:           filepath.Join("data", "usage-buffer"),
		},
		Streams: StreamNamesConfig{
			SessionEvents:       "realtime-interview-core:session.events",
			SpeechMetrics:       "realtime-interview-core:speech.metrics",
			OrchestratorOutputs: "realtime-interview-core:orchestrator.outputs",
			Usage:               "realtime-interview-core:usage",
			Control:             "realtime-interview-core:control",
		},
	}
}

func (c *Config) applyEnvOverrides() {
	overrideString(&c.Env, "REALTIME_INTERVIEW_ENV")
	overrideString(&c.Server.Address, "REALTIME_INTERVIEW_SERVER_ADDR")
	overrideString(&c.Server.GRPCAddress, "REALTIME_INTERVIEW_SERVER_GRPC_ADDR")
	overrideString(&c.Logging.Level, "REALTIME_INTERVIEW_LOG_LEVEL")
	if v := strings.TrimSpace(os.Getenv("REALTIME_INTERVIEW_LOG_PRETTY")); v != "" {
		c.Logging.Pretty = strings.EqualFold(v, "true") || v == "1"
	}
	overrideString(&c.Telemetry.PrometheusAddr, "REALTIME_INTERVIEW_PROM_ADDR")
	overrideString(&c.Telemetry.EventStream, "REALTIME_INTERVIEW_TELEM_STREAM")
	overrideString(&c.Telemetry.BufferPath, "REALTIME_INTERVIEW_TELEM_BUFFER_PATH")
	overrideInt(&c.Telemetry.FlushIntervalSeconds, "REALTIME_INTERVIEW_TELEM_FLUSH_SECONDS")
	overrideInt64(&c.Telemetry.StreamMaxLen, "REALTIME_INTERVIEW_TELEM_STREAM_MAX_LEN")
	overrideString(&c.Database.DSN, "REALTIME_INTERVIEW_DB_DSN")
	overrideInt(&c.Database.MaxOpenConns, "REALTIME_INTERVIEW_DB_MAX_OPEN")
	overrideInt(&c.Database.MaxIdleConns, "REALTIME_INTERVIEW_DB_MAX_IDLE")
	overrideInt(&c.Database.ConnMaxLifetimeSeconds, "REALTIME_INTERVIEW_DB_CONN_MAX_LIFETIME")
	overrideString(&c.Redis.Addr, "REALTIME_INTERVIEW_REDIS_ADDR")
	overrideString(&c.Redis.Password, "REALTIME_INTERVIEW_REDIS_PASSWORD")
	overrideInt(&c.Redis.DB, "REALTIME_INTERVIEW_REDIS_DB")
	overrideString(&c.IAMClient.Address, "REALTIME_INTERVIEW_IAM_ADDR")
	overrideInt(&c.IAMClient.TimeoutSeconds, "REALTIME_INTERVIEW_IAM_TIMEOUT")
	if v := strings.TrimSpace(os.Getenv("REALTIME_INTERVIEW_IAM_INSECURE")); v != "" {
		c.IAMClient.Insecure = strings.EqualFold(v, "true") || v == "1"
	}
	overrideInt(&c.Speech.MaxChunkBytes, "REALTIME_INTERVIEW_SPEECH_MAX_CHUNK")
	overrideInt(&c.Speech.AckDeadlineMillis, "REALTIME_INTERVIEW_SPEECH_ACK_MS")
	overrideInt(&c.Speech.SessionIdleTimeoutSeconds, "REALTIME_INTERVIEW_SPEECH_IDLE_SEC")
	overrideString(&c.Speech.BufferPrefix, "REALTIME_INTERVIEW_SPEECH_BUFFER_PREFIX")
	overrideInt(&c.Speech.BufferTTLSeconds, "REALTIME_INTERVIEW_SPEECH_BUFFER_TTL")
	overrideInt(&c.Speech.CircuitBreakerFailures, "REALTIME_INTERVIEW_SPEECH_BREAKER_FAILS")
	overrideInt(&c.Speech.CircuitBreakerResetSeconds, "REALTIME_INTERVIEW_SPEECH_BREAKER_RESET")
	overrideString(&c.Providers.Default, "REALTIME_INTERVIEW_PROVIDER_DEFAULT")
	overrideString(&c.Providers.Aliyun.APIURL, "REALTIME_INTERVIEW_PROVIDER_ALIYUN_API_URL")
	overrideString(&c.Providers.Aliyun.Domain, "REALTIME_INTERVIEW_PROVIDER_ALIYUN_DOMAIN")
	overrideString(&c.Providers.Aliyun.AppKey, "REALTIME_INTERVIEW_PROVIDER_ALIYUN_APP_KEY")
	overrideString(&c.Providers.Aliyun.Token, "REALTIME_INTERVIEW_PROVIDER_ALIYUN_TOKEN")
	overrideString(&c.Providers.Aliyun.TokenURL, "REALTIME_INTERVIEW_PROVIDER_ALIYUN_TOKEN_URL")
	overrideString(&c.Providers.Aliyun.Format, "REALTIME_INTERVIEW_PROVIDER_ALIYUN_FORMAT")
	overrideInt(&c.Providers.Aliyun.SampleRate, "REALTIME_INTERVIEW_PROVIDER_ALIYUN_SAMPLE_RATE")
	if v := strings.TrimSpace(os.Getenv("REALTIME_INTERVIEW_PROVIDER_ALIYUN_INTERMEDIATE")); v != "" {
		c.Providers.Aliyun.EnableIntermediateResult = strings.EqualFold(v, "true") || v == "1"
	}
	if v := strings.TrimSpace(os.Getenv("REALTIME_INTERVIEW_PROVIDER_ALIYUN_ITN")); v != "" {
		c.Providers.Aliyun.EnableInverseTextNormalization = strings.EqualFold(v, "true") || v == "1"
	}
	if v := strings.TrimSpace(os.Getenv("REALTIME_INTERVIEW_PROVIDER_ALIYUN_PUNCT")); v != "" {
		c.Providers.Aliyun.EnablePunctuation = strings.EqualFold(v, "true") || v == "1"
	}
	if v := strings.TrimSpace(os.Getenv("REALTIME_INTERVIEW_PROVIDER_ALIYUN_VAD")); v != "" {
		c.Providers.Aliyun.EnableVoiceDetection = strings.EqualFold(v, "true") || v == "1"
	}
	overrideString(&c.Orchestrator.Provider, "REALTIME_INTERVIEW_ORCH_PROVIDER")
	overrideString(&c.Orchestrator.ShortReplyTemplate, "REALTIME_INTERVIEW_ORCH_SHORT_TEMPLATE")
	overrideString(&c.Orchestrator.DetailedTemplate, "REALTIME_INTERVIEW_ORCH_DETAILED_TEMPLATE")
	overrideString(&c.Orchestrator.CoachHintTemplate, "REALTIME_INTERVIEW_ORCH_HINT_TEMPLATE")
	overrideInt(&c.Orchestrator.BudgetWindowSeconds, "REALTIME_INTERVIEW_ORCH_BUDGET_WINDOW")
	overrideInt(&c.Orchestrator.MaxTokensPerWindow, "REALTIME_INTERVIEW_ORCH_MAX_TOKENS")
	overrideString(&c.Orchestrator.QueueStream, "REALTIME_INTERVIEW_ORCH_QUEUE_STREAM")
	overrideString(&c.Orchestrator.Providers.OpenAI.APIKey, "REALTIME_INTERVIEW_ORCH_OPENAI_API_KEY")
	overrideString(&c.Orchestrator.Providers.OpenAI.BaseURL, "REALTIME_INTERVIEW_ORCH_OPENAI_BASE_URL")
	overrideString(&c.Orchestrator.Providers.OpenAI.Model, "REALTIME_INTERVIEW_ORCH_OPENAI_MODEL")
	overrideInt(&c.Orchestrator.Providers.OpenAI.TimeoutSeconds, "REALTIME_INTERVIEW_ORCH_OPENAI_TIMEOUT")
	overrideString(&c.Orchestrator.Providers.DeepSeek.APIKey, "REALTIME_INTERVIEW_ORCH_DEEPSEEK_API_KEY")
	overrideString(&c.Orchestrator.Providers.DeepSeek.BaseURL, "REALTIME_INTERVIEW_ORCH_DEEPSEEK_BASE_URL")
	overrideString(&c.Orchestrator.Providers.DeepSeek.Model, "REALTIME_INTERVIEW_ORCH_DEEPSEEK_MODEL")
	overrideInt(&c.Orchestrator.Providers.DeepSeek.TimeoutSeconds, "REALTIME_INTERVIEW_ORCH_DEEPSEEK_TIMEOUT")
	overrideString(&c.Orchestrator.Providers.GLM.APIKey, "REALTIME_INTERVIEW_ORCH_GLM_API_KEY")
	overrideString(&c.Orchestrator.Providers.GLM.BaseURL, "REALTIME_INTERVIEW_ORCH_GLM_BASE_URL")
	overrideString(&c.Orchestrator.Providers.GLM.Model, "REALTIME_INTERVIEW_ORCH_GLM_MODEL")
	overrideInt(&c.Orchestrator.Providers.GLM.TimeoutSeconds, "REALTIME_INTERVIEW_ORCH_GLM_TIMEOUT")
	overrideInt(&c.Usage.FlushIntervalSeconds, "REALTIME_INTERVIEW_USAGE_FLUSH_SECONDS")
	overrideInt(&c.Usage.BatchSize, "REALTIME_INTERVIEW_USAGE_BATCH_SIZE")
	overrideString(&c.Usage.BufferPath, "REALTIME_INTERVIEW_USAGE_BUFFER_PATH")
	overrideString(&c.Streams.SessionEvents, "REALTIME_INTERVIEW_STREAM_SESSION_EVENTS")
	overrideString(&c.Streams.SpeechMetrics, "REALTIME_INTERVIEW_STREAM_SPEECH_METRICS")
	overrideString(&c.Streams.OrchestratorOutputs, "REALTIME_INTERVIEW_STREAM_ORCH_OUTPUTS")
	overrideString(&c.Streams.Usage, "REALTIME_INTERVIEW_STREAM_USAGE")
	overrideString(&c.Streams.Control, "REALTIME_INTERVIEW_STREAM_CONTROL")
}

func overrideString(target *string, key string) {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		*target = v
	}
}

func overrideInt(target *int, key string) {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			*target = parsed
		}
	}
}

func overrideInt64(target *int64, key string) {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			*target = parsed
		}
	}
}
