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
	envConfigPathKey  = "CLOUD_ACCESS_CONFIG"
)

// Config aggregates runtime configuration values shared across modules.
type Config struct {
	Env       string             `json:"env"`
	Server    ServerConfig       `json:"server"`
	Logging   LoggingConfig      `json:"logging"`
	Telemetry TelemetryConfig    `json:"telemetry"`
	Database  DatabaseConfig     `json:"database"`
	Redis     RedisConfig        `json:"redis"`
	Auth      AuthConfig         `json:"auth"`
	Device    DeviceConfig       `json:"device"`
	Policy    PolicyConfig       `json:"policy"`
	Gateway   GatewayConfig      `json:"gateway"`
	Config    ConfigCenterConfig `json:"config_center"`
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
		return "cloud-access-core:audit"
	}
	return c.EventStream
}

// KafkaTopicName returns the configured Kafka topic placeholder.
func (c TelemetryConfig) KafkaTopicName() string {
	if strings.TrimSpace(c.KafkaTopic) == "" {
		return "cloud-access-core.audit"
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

// ConnMaxLifetime returns the configured connection lifetime or a safe default.
func (c DatabaseConfig) ConnMaxLifetime() time.Duration {
	if c.ConnMaxLifetimeSeconds <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(c.ConnMaxLifetimeSeconds) * time.Second
}

// AccessTokenTTL returns the access token TTL or default 15 minutes.
func (c AuthConfig) AccessTokenTTL() time.Duration {
	if c.AccessTokenTTLSeconds <= 0 {
		return 15 * time.Minute
	}
	return time.Duration(c.AccessTokenTTLSeconds) * time.Second
}

// DeviceTokenTTL returns the device token TTL or mirrors access TTL if unset.
func (c AuthConfig) DeviceTokenTTL() time.Duration {
	if c.DeviceTokenTTLSeconds <= 0 {
		return c.AccessTokenTTL()
	}
	return time.Duration(c.DeviceTokenTTLSeconds) * time.Second
}

// RefreshTokenTTL returns the refresh token TTL or defaults to 7 days.
func (c AuthConfig) RefreshTokenTTL() time.Duration {
	if c.RefreshTokenTTLSeconds <= 0 {
		return 7 * 24 * time.Hour
	}
	return time.Duration(c.RefreshTokenTTLSeconds) * time.Second
}

// PairingCodeTTL returns TTL for device pairing challenges.
func (c DeviceConfig) PairingCodeTTL() time.Duration {
	if c.PairingCodeTTLSeconds <= 0 {
		return 10 * time.Minute
	}
	return time.Duration(c.PairingCodeTTLSeconds) * time.Second
}

// CodeLength sanitizes pairing code length.
func (c DeviceConfig) CodeLength() int {
	if c.PairingCodeLength <= 0 {
		return 8
	}
	if c.PairingCodeLength > 32 {
		return 32
	}
	return c.PairingCodeLength
}

// QuotaWindow returns the quota window duration or default 24h.
func (c PolicyConfig) QuotaWindow() time.Duration {
	if c.QuotaWindowSeconds <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(c.QuotaWindowSeconds) * time.Second
}

// DatabaseConfig controls GORM/PostgreSQL connectivity.
type DatabaseConfig struct {
	DSN                    string `json:"dsn"`
	MaxOpenConns           int    `json:"max_open_conns"`
	MaxIdleConns           int    `json:"max_idle_conns"`
	ConnMaxLifetimeSeconds int    `json:"conn_max_lifetime_seconds"`
}

// RedisConfig configures the session/event cache connection.
type RedisConfig struct {
	Addr     string `json:"addr"`
	Password string `json:"password"`
	DB       int    `json:"db"`
}

// AuthConfig defines IAM token, signer, and session behaviour.
type AuthConfig struct {
	Issuer                 string        `json:"issuer"`
	WebAudience            string        `json:"web_audience"`
	DeviceAudience         string        `json:"device_audience"`
	AccessTokenTTLSeconds  int           `json:"access_token_ttl_seconds"`
	DeviceTokenTTLSeconds  int           `json:"device_token_ttl_seconds"`
	RefreshTokenTTLSeconds int           `json:"refresh_token_ttl_seconds"`
	SessionPrefix          string        `json:"session_prefix"`
	Signing                SigningConfig `json:"signing"`
}

// DeviceConfig holds device onboarding + heartbeat settings.
type DeviceConfig struct {
	PairingCodeTTLSeconds int    `json:"pairing_code_ttl_seconds"`
	PairingCodeLength     int    `json:"pairing_code_length"`
	PairingStorePrefix    string `json:"pairing_store_prefix"`
	ProfileKeyBase64      string `json:"profile_key_base64"`
	DefaultProfileName    string `json:"default_profile_name"`
	HeartbeatStream       string `json:"heartbeat_stream"`
	HeartbeatGroup        string `json:"heartbeat_group"`
	RevocationChannel     string `json:"revocation_channel"`
}

// ConfigCenterConfig controls config-center behaviour.
type ConfigCenterConfig struct {
	UpdateChannelPrefix string `json:"update_channel_prefix"`
	SigningKeyBase64    string `json:"signing_key_base64"`
}

// PolicyConfig configures the policy engine and quota defaults.
type PolicyConfig struct {
	BucketPrefix       string `json:"bucket_prefix"`
	UpdateChannel      string `json:"update_channel"`
	QuotaWindowSeconds int    `json:"quota_window_seconds"`
	GRPCAddress        string `json:"grpc_address"`
}

// GatewayConfig controls API gateway behaviour.
type GatewayConfig struct {
	Enabled              bool    `json:"enabled"`
	RoutesPath           string  `json:"routes_path"`
	AdminToken           string  `json:"admin_token"`
	RateLimitPrefix      string  `json:"rate_limit_prefix"`
	DefaultCapacity      float64 `json:"default_capacity"`
	DefaultRefillPerSec  float64 `json:"default_refill_per_second"`
	DefaultWindowSeconds int     `json:"default_window_seconds"`
	CertBundlePath       string  `json:"cert_bundle_path"`
}

// SigningConfig holds Ed25519 key material for JWT/PASETO.
type SigningConfig struct {
	KeyID            string `json:"key_id"`
	PrivateKeyBase64 string `json:"private_key_base64"`
	PublicKeyBase64  string `json:"public_key_base64"`
}

// ShutdownTimeout returns the configured grace period or a safe default.
func (s ServerConfig) ShutdownTimeout() time.Duration {
	if s.ShutdownGraceSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(s.ShutdownGraceSeconds) * time.Second
}

// LoadConfigFromEnv loads configuration using the CLOUD_ACCESS_CONFIG env variable if present.
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
		filepath.Join("..", "..", defaultConfigPath), // allow running from service directory
		filepath.Join("..", defaultConfigPath),       // allow running from cmd directory
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
			Address:              ":8080",
			GRPCAddress:          ":9091",
			ShutdownGraceSeconds: 15,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Pretty: true,
		},
		Telemetry: TelemetryConfig{
			PrometheusAddr:       ":9090",
			EventStream:          "cloud-access-core:audit",
			KafkaTopic:           "cloud-access-core.audit",
			BufferPath:           filepath.Join("data", "telemetry-buffer"),
			FlushIntervalSeconds: 5,
			StreamMaxLen:         1000,
		},
		Database: DatabaseConfig{
			DSN:                    "postgres://postgres:postgres@localhost:5432/cloud_access?sslmode=disable",
			MaxOpenConns:           10,
			MaxIdleConns:           5,
			ConnMaxLifetimeSeconds: 1800,
		},
		Redis: RedisConfig{
			Addr: "127.0.0.1:6379",
			DB:   0,
		},
		Auth: AuthConfig{
			Issuer:                 "cloud-access-core",
			WebAudience:            "cloud-access-web",
			DeviceAudience:         "cloud-access-device",
			AccessTokenTTLSeconds:  900,
			DeviceTokenTTLSeconds:  900,
			RefreshTokenTTLSeconds: 604800,
			SessionPrefix:          "cloud-access-core",
			Signing: SigningConfig{
				KeyID: "dev-key",
			},
		},
		Device: DeviceConfig{
			PairingCodeTTLSeconds: 600,
			PairingCodeLength:     8,
			PairingStorePrefix:    "cloud-access-core:pairing",
			DefaultProfileName:    "default",
			HeartbeatStream:       "cloud-access-core:device-heartbeats",
			HeartbeatGroup:        "iam-heartbeat",
			RevocationChannel:     "cloud-access-core:device-revocations",
		},
		Policy: PolicyConfig{
			BucketPrefix:       "cloud-access-core:quota",
			UpdateChannel:      "cloud-access-core:policy-updated",
			QuotaWindowSeconds: 86400,
			GRPCAddress:        ":9092",
		},
		Gateway: GatewayConfig{
			Enabled:              true,
			RoutesPath:           "configs/gateway.routes.yaml",
			AdminToken:           "dev-admin-token",
			RateLimitPrefix:      "cloud-access-core:gateway",
			DefaultCapacity:      100,
			DefaultRefillPerSec:  50,
			DefaultWindowSeconds: 60,
			CertBundlePath:       "configs/gateway.certbundle.pem",
		},
		Config: ConfigCenterConfig{
			UpdateChannelPrefix: "cloud-access-core:config-updates",
		},
	}
}

func (c *Config) applyEnvOverrides() {
	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_ENV")); v != "" {
		c.Env = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_SERVER_ADDR")); v != "" {
		c.Server.Address = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_GRPC_ADDR")); v != "" {
		c.Server.GRPCAddress = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_LOG_LEVEL")); v != "" {
		c.Logging.Level = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_LOG_PRETTY")); v != "" {
		c.Logging.Pretty = strings.EqualFold(v, "true") || v == "1"
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_PROM_ADDR")); v != "" {
		c.Telemetry.PrometheusAddr = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_TELEM_STREAM")); v != "" {
		c.Telemetry.EventStream = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_TELEM_KAFKA_TOPIC")); v != "" {
		c.Telemetry.KafkaTopic = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_TELEM_BUFFER_PATH")); v != "" {
		c.Telemetry.BufferPath = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_TELEM_FLUSH_SECONDS")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Telemetry.FlushIntervalSeconds = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_TELEM_STREAM_MAXLEN")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Telemetry.StreamMaxLen = int64(parsed)
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DB_DSN")); v != "" {
		c.Database.DSN = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DB_MAX_OPEN")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Database.MaxOpenConns = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DB_MAX_IDLE")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Database.MaxIdleConns = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DB_CONN_LIFETIME")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Database.ConnMaxLifetimeSeconds = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_REDIS_ADDR")); v != "" {
		c.Redis.Addr = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_REDIS_PASSWORD")); v != "" {
		c.Redis.Password = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_REDIS_DB")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Redis.DB = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_AUTH_ISSUER")); v != "" {
		c.Auth.Issuer = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_AUTH_WEB_AUD")); v != "" {
		c.Auth.WebAudience = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_AUTH_DEVICE_AUD")); v != "" {
		c.Auth.DeviceAudience = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_AUTH_ACCESS_TTL")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Auth.AccessTokenTTLSeconds = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_AUTH_DEVICE_TTL")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Auth.DeviceTokenTTLSeconds = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_AUTH_REFRESH_TTL")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Auth.RefreshTokenTTLSeconds = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_AUTH_SESSION_PREFIX")); v != "" {
		c.Auth.SessionPrefix = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_AUTH_SIGNING_KEY_ID")); v != "" {
		c.Auth.Signing.KeyID = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_AUTH_SIGNING_PRIV")); v != "" {
		c.Auth.Signing.PrivateKeyBase64 = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_AUTH_SIGNING_PUB")); v != "" {
		c.Auth.Signing.PublicKeyBase64 = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DEVICE_PAIRING_TTL")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Device.PairingCodeTTLSeconds = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DEVICE_PAIRING_LENGTH")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Device.PairingCodeLength = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DEVICE_PAIRING_PREFIX")); v != "" {
		c.Device.PairingStorePrefix = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DEVICE_PROFILE_KEY")); v != "" {
		c.Device.ProfileKeyBase64 = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DEVICE_PROFILE_NAME")); v != "" {
		c.Device.DefaultProfileName = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DEVICE_HEARTBEAT_STREAM")); v != "" {
		c.Device.HeartbeatStream = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DEVICE_HEARTBEAT_GROUP")); v != "" {
		c.Device.HeartbeatGroup = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_DEVICE_REVOCATION_CHANNEL")); v != "" {
		c.Device.RevocationChannel = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_CONFIG_UPDATE_PREFIX")); v != "" {
		c.Config.UpdateChannelPrefix = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_CONFIG_SIGNING_KEY")); v != "" {
		c.Config.SigningKeyBase64 = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_POLICY_BUCKET_PREFIX")); v != "" {
		c.Policy.BucketPrefix = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_POLICY_UPDATE_CHANNEL")); v != "" {
		c.Policy.UpdateChannel = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_POLICY_QUOTA_WINDOW")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Policy.QuotaWindowSeconds = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_POLICY_GRPC_ADDR")); v != "" {
		c.Policy.GRPCAddress = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_GATEWAY_ENABLED")); v != "" {
		c.Gateway.Enabled = strings.EqualFold(v, "true") || v == "1"
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_GATEWAY_ROUTES_PATH")); v != "" {
		c.Gateway.RoutesPath = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_GATEWAY_ADMIN_TOKEN")); v != "" {
		c.Gateway.AdminToken = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_GATEWAY_RATE_PREFIX")); v != "" {
		c.Gateway.RateLimitPrefix = v
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_GATEWAY_DEFAULT_CAPACITY")); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			c.Gateway.DefaultCapacity = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_GATEWAY_DEFAULT_REFILL")); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			c.Gateway.DefaultRefillPerSec = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_GATEWAY_DEFAULT_WINDOW")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			c.Gateway.DefaultWindowSeconds = parsed
		}
	}

	if v := strings.TrimSpace(os.Getenv("CLOUD_ACCESS_GATEWAY_CERT_PATH")); v != "" {
		c.Gateway.CertBundlePath = v
	}
}
