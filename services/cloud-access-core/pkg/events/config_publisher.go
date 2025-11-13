package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ConfigUpdateEvent describes a profile change notification payload.
type ConfigUpdateEvent struct {
	ProfileName string         `json:"profile_name"`
	Version     int            `json:"version"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Timestamp   string         `json:"timestamp"`
}

// ConfigPublisher broadcasts profile changes to interested subscribers.
type ConfigPublisher interface {
	PublishProfileUpdate(ctx context.Context, tenantID uuid.UUID, event ConfigUpdateEvent) error
}

// RedisConfigPublisher uses Redis Pub/Sub for config updates.
type RedisConfigPublisher struct {
	client *redis.Client
	prefix string
	logger *slog.Logger
}

// NewRedisConfigPublisher constructs a Redis-backed config publisher.
func NewRedisConfigPublisher(client *redis.Client, prefix string, logger *slog.Logger) *RedisConfigPublisher {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "cloud-access-core:config-updates"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RedisConfigPublisher{
		client: client,
		prefix: prefix,
		logger: logger.With("component", "config_publisher"),
	}
}

// PublishProfileUpdate sends the event to the tenant-specific channel.
func (p *RedisConfigPublisher) PublishProfileUpdate(ctx context.Context, tenantID uuid.UUID, event ConfigUpdateEvent) error {
	if p == nil || p.client == nil {
		return nil
	}
	if event.Timestamp == "" {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	payload := map[string]any{
		"tenant_id": tenantID.String(),
		"event":     event,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal config update: %w", err)
	}
	if err := p.client.Publish(ctx, p.channel(tenantID), data).Err(); err != nil {
		if p.logger != nil {
			p.logger.Warn("failed to publish config update", "error", err)
		}
		return err
	}
	return nil
}

func (p *RedisConfigPublisher) channel(tenantID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", p.prefix, tenantID.String())
}

// ChannelForTenant exposes the full Redis channel name for subscribers.
func (p *RedisConfigPublisher) ChannelForTenant(tenantID uuid.UUID) string {
	return p.channel(tenantID)
}
