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

// PolicyPublisher notifies subscribers about policy changes.
type PolicyPublisher interface {
	PublishPolicyUpdate(ctx context.Context, tenantID uuid.UUID, metadata map[string]any) error
}

// RedisPolicyPublisher publishes events via Redis Pub/Sub.
type RedisPolicyPublisher struct {
	client  *redis.Client
	channel string
	logger  *slog.Logger
}

// NewRedisPolicyPublisher builds a Redis-backed policy publisher.
func NewRedisPolicyPublisher(client *redis.Client, channel string, logger *slog.Logger) *RedisPolicyPublisher {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = "cloud-access-core:policy-updated"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RedisPolicyPublisher{client: client, channel: channel, logger: logger.With("component", "policy_publisher")}
}

// PublishPolicyUpdate sends a JSON payload to the configured channel.
func (p *RedisPolicyPublisher) PublishPolicyUpdate(ctx context.Context, tenantID uuid.UUID, metadata map[string]any) error {
	if p == nil || p.client == nil {
		return nil
	}
	payload := map[string]any{
		"tenant_id":  tenantID.String(),
		"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
		"metadata":   metadata,
		"event_type": "policy.updated",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal policy update: %w", err)
	}
	if err := p.client.Publish(ctx, p.channel, data).Err(); err != nil {
		if p.logger != nil {
			p.logger.Warn("failed to publish policy update", "error", err)
		}
		return err
	}
	return nil
}
