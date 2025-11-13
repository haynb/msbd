package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ConfigSubscriber consumes profile update events for tenants.
type ConfigSubscriber interface {
	Subscribe(ctx context.Context, tenantID uuid.UUID) (<-chan ConfigUpdateEvent, func(), error)
}

// RedisConfigSubscriber implements ConfigSubscriber via Redis Pub/Sub.
type RedisConfigSubscriber struct {
	client *redis.Client
	prefix string
	logger *slog.Logger
}

// NewRedisConfigSubscriber builds a Redis-backed subscriber.
func NewRedisConfigSubscriber(client *redis.Client, prefix string, logger *slog.Logger) *RedisConfigSubscriber {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "cloud-access-core:config-updates"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RedisConfigSubscriber{client: client, prefix: prefix, logger: logger.With("component", "config_subscriber")}
}

// Subscribe begins streaming events for the tenant until the cancel func is invoked.
func (s *RedisConfigSubscriber) Subscribe(ctx context.Context, tenantID uuid.UUID) (<-chan ConfigUpdateEvent, func(), error) {
	if s == nil || s.client == nil {
		ch := make(chan ConfigUpdateEvent)
		close(ch)
		return ch, func() {}, nil
	}
	cctx, cancel := context.WithCancel(ctx)
	pubsub := s.client.Subscribe(cctx, s.channel(tenantID))
	if _, err := pubsub.Receive(cctx); err != nil {
		cancel()
		return nil, func() {}, fmt.Errorf("config subscribe: %w", err)
	}
	out := make(chan ConfigUpdateEvent, 1)
	go func() {
		defer close(out)
		defer cancel()
		defer pubsub.Close()
		for {
			msg, err := pubsub.ReceiveMessage(cctx)
			if err != nil {
				if cctx.Err() == nil && s.logger != nil {
					s.logger.Warn("config subscriber stopped", "error", err)
				}
				return
			}
			var payload struct {
				Event ConfigUpdateEvent `json:"event"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &payload); err != nil {
				if s.logger != nil {
					s.logger.Warn("failed to decode config event", "error", err)
				}
				continue
			}
			select {
			case out <- payload.Event:
			case <-cctx.Done():
				return
			}
		}
	}()
	cleanup := func() {
		cancel()
		pubsub.Close()
	}
	return out, cleanup, nil
}

func (s *RedisConfigSubscriber) channel(tenantID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", s.prefix, tenantID.String())
}
