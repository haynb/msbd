package speechgateway

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
)

// RuntimeConfig represents dynamic overrides applied without restarts.
type RuntimeConfig struct {
	DefaultProvider string               `json:"default_provider"`
	Aliyun          *AliyunRuntimeConfig `json:"aliyun,omitempty"`
}

// AliyunRuntimeConfig overrides Aliyun adapter fields at runtime.
type AliyunRuntimeConfig struct {
	APIURL                   string `json:"api_url,omitempty"`
	Domain                   string `json:"domain,omitempty"`
	AppKey                   string `json:"app_key,omitempty"`
	Token                    string `json:"token,omitempty"`
	TokenURL                 string `json:"token_url,omitempty"`
	Format                   string `json:"format,omitempty"`
	SampleRate               *int   `json:"sample_rate,omitempty"`
	EnableIntermediateResult *bool  `json:"enable_intermediate_result,omitempty"`
	EnableITN                *bool  `json:"enable_itn,omitempty"`
	EnablePunctuation        *bool  `json:"enable_punctuation,omitempty"`
	EnableVoiceDetection     *bool  `json:"enable_voice_detection,omitempty"`
}

// normalize trims whitespace and ensures casing.
func (c RuntimeConfig) normalize() RuntimeConfig {
	cfg := c
	cfg.DefaultProvider = strings.ToLower(strings.TrimSpace(cfg.DefaultProvider))
	if cfg.Aliyun != nil {
		alias := *cfg.Aliyun
		alias.APIURL = strings.TrimSpace(alias.APIURL)
		alias.Domain = strings.TrimSpace(alias.Domain)
		alias.AppKey = strings.TrimSpace(alias.AppKey)
		alias.Token = strings.TrimSpace(alias.Token)
		alias.TokenURL = strings.TrimSpace(alias.TokenURL)
		alias.Format = strings.TrimSpace(alias.Format)
		cfg.Aliyun = &alias
	}
	return cfg
}

// mergedAliyun overlays runtime fields on top of the base config.
func (c RuntimeConfig) mergedAliyun(base bootstrap.AliyunProviderConfig) bootstrap.AliyunProviderConfig {
	cfg := base
	if c.Aliyun == nil {
		return cfg
	}
	if v := c.Aliyun.APIURL; v != "" {
		cfg.APIURL = v
	}
	if v := c.Aliyun.Domain; v != "" {
		cfg.Domain = v
	}
	if v := c.Aliyun.AppKey; v != "" {
		cfg.AppKey = v
	}
	if v := c.Aliyun.Token; v != "" {
		cfg.Token = v
	}
	if v := c.Aliyun.TokenURL; v != "" {
		cfg.TokenURL = v
	}
	if v := c.Aliyun.Format; v != "" {
		cfg.Format = v
	}
	if v := c.Aliyun.SampleRate; v != nil {
		cfg.SampleRate = *v
	}
	if v := c.Aliyun.EnableIntermediateResult; v != nil {
		cfg.EnableIntermediateResult = *v
	}
	if v := c.Aliyun.EnableITN; v != nil {
		cfg.EnableInverseTextNormalization = *v
	}
	if v := c.Aliyun.EnablePunctuation; v != nil {
		cfg.EnablePunctuation = *v
	}
	if v := c.Aliyun.EnableVoiceDetection; v != nil {
		cfg.EnableVoiceDetection = *v
	}
	return cfg
}

// ConfigStore persists runtime overrides inside Redis and fan-outs updates via Pub/Sub.
type ConfigStore struct {
	client  *redis.Client
	key     string
	channel string
	logger  *slog.Logger
}

// NewConfigStore constructs a ConfigStore instance.
func NewConfigStore(client *redis.Client, key, channel string, logger *slog.Logger) *ConfigStore {
	if client == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "realtime-interview-core:speech-gateway:runtime"
	}
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "realtime-interview-core:speech-gateway:updates"
	}
	return &ConfigStore{client: client, key: key, channel: channel, logger: logger.With("component", "speech.runtime.store")}
}

// Load returns the stored runtime config.
func (s *ConfigStore) Load(ctx context.Context) (RuntimeConfig, error) {
	var cfg RuntimeConfig
	if s == nil || s.client == nil {
		return cfg, nil
	}
	data, err := s.client.Get(ctx, s.key).Bytes()
	if errors.Is(err, redis.Nil) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return RuntimeConfig{}, err
	}
	return cfg.normalize(), nil
}

// Save persists the runtime config and broadcasts an update.
func (s *ConfigStore) Save(ctx context.Context, cfg RuntimeConfig) error {
	if s == nil || s.client == nil {
		return errors.New("runtime store not configured")
	}
	payload, err := json.Marshal(cfg.normalize())
	if err != nil {
		return err
	}
	if err := s.client.Set(ctx, s.key, payload, 0).Err(); err != nil {
		return err
	}
	if err := s.client.Publish(ctx, s.channel, payload).Err(); err != nil && !errors.Is(err, redis.Nil) {
		if s.logger != nil {
			s.logger.Warn("runtime config publish failed", "error", err)
		}
	}
	return nil
}

// Subscribe streams runtime updates until the context is canceled.
func (s *ConfigStore) Subscribe(ctx context.Context) (<-chan RuntimeConfig, func(), error) {
	if s == nil || s.client == nil {
		ch := make(chan RuntimeConfig)
		close(ch)
		return ch, func() {}, nil
	}
	pubsub := s.client.Subscribe(ctx, s.channel)
	if _, err := pubsub.Receive(ctx); err != nil {
		pubsub.Close()
		return nil, func() {}, err
	}
	out := make(chan RuntimeConfig, 1)
	go func() {
		defer close(out)
		defer pubsub.Close()
		for {
			msg, err := pubsub.ReceiveMessage(ctx)
			if err != nil {
				if ctx.Err() == nil && s.logger != nil {
					s.logger.Warn("runtime config subscriber stopped", "error", err)
				}
				return
			}
			var cfg RuntimeConfig
			if err := json.Unmarshal([]byte(msg.Payload), &cfg); err != nil {
				if s.logger != nil {
					s.logger.Warn("runtime config decode failed", "error", err)
				}
				continue
			}
			select {
			case out <- cfg.normalize():
			case <-ctx.Done():
				return
			}
		}
	}()
	cleanup := func() { _ = pubsub.Close() }
	return out, cleanup, nil
}

// ProviderAdapters exposes the set of configurable providers.
type ProviderAdapters struct {
	Aliyun AliyunConfigurator
}

// AliyunConfigurator describes the subset of adapter methods needed for runtime overrides.
type AliyunConfigurator interface {
	UpdateConfig(cfg bootstrap.AliyunProviderConfig) error
}

// ApplyRuntimeConfig pushes runtime overrides into the speech gateway + adapters.
func ApplyRuntimeConfig(cfg RuntimeConfig, base bootstrap.ProvidersConfig, gateway *Service, adapters ProviderAdapters, logger *slog.Logger) {
	if gateway == nil {
		return
	}
	runtime := cfg.normalize()
	if runtime.DefaultProvider != "" {
		gateway.SetDefaultProvider(runtime.DefaultProvider)
	}
	if adapters.Aliyun != nil {
		merged := runtime.mergedAliyun(base.Aliyun)
		if err := adapters.Aliyun.UpdateConfig(merged); err != nil && logger != nil {
			logger.Warn("aliyun runtime update failed", "error", err)
		}
	}
}

// InitializeRuntimeConfig applies stored overrides and starts the watcher loop.
func InitializeRuntimeConfig(ctx context.Context, store *ConfigStore, base bootstrap.ProvidersConfig, gateway *Service, adapters ProviderAdapters, logger *slog.Logger) {
	if store == nil || gateway == nil {
		return
	}
	if cfg, err := store.Load(ctx); err != nil {
		if logger != nil {
			logger.Warn("load runtime speech config failed", "error", err)
		}
	} else {
		ApplyRuntimeConfig(cfg, base, gateway, adapters, logger)
	}
	updates, cleanup, err := store.Subscribe(ctx)
	if err != nil {
		if logger != nil {
			logger.Warn("subscribe runtime speech config failed", "error", err)
		}
		return
	}
	go func() {
		defer cleanup()
		for {
			select {
			case <-ctx.Done():
				return
			case cfg, ok := <-updates:
				if !ok {
					return
				}
				ApplyRuntimeConfig(cfg, base, gateway, adapters, logger)
			}
		}
	}()
}
