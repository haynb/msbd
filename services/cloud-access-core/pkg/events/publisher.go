package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Event represents a structured payload published to downstream systems.
type Event struct {
	Topic     string         `json:"topic"`
	Payload   map[string]any `json:"payload"`
	Timestamp time.Time      `json:"timestamp"`
}

// Publisher defines the contract for emitting events.
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

// NewCompositePublisher fans out publish calls to all non-nil publishers.
func NewCompositePublisher(publishers ...Publisher) Publisher {
	filtered := make([]Publisher, 0, len(publishers))
	for _, p := range publishers {
		if p != nil {
			filtered = append(filtered, p)
		}
	}
	switch len(filtered) {
	case 0:
		return noopPublisher{}
	case 1:
		return filtered[0]
	default:
		return &compositePublisher{publishers: filtered}
	}
}

type compositePublisher struct {
	publishers []Publisher
}

func (c *compositePublisher) Publish(ctx context.Context, event Event) error {
	var publishErr error
	for _, p := range c.publishers {
		if err := p.Publish(ctx, event); err != nil {
			publishErr = err
		}
	}
	return publishErr
}

type noopPublisher struct{}

func (noopPublisher) Publish(ctx context.Context, event Event) error { return nil }

// RedisStreamPublisher appends JSON payloads to a Redis Stream.
type RedisStreamPublisher struct {
	client *redis.Client
	stream string
	maxLen int64
	logger *slog.Logger
}

// NewRedisStreamPublisher constructs a Redis-backed publisher.
func NewRedisStreamPublisher(client *redis.Client, stream string, maxLen int64, logger *slog.Logger) *RedisStreamPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	stream = strings.TrimSpace(stream)
	return &RedisStreamPublisher{client: client, stream: stream, maxLen: maxLen, logger: logger.With("component", "redis_publisher")}
}

// Publish writes the event into the configured stream.
func (p *RedisStreamPublisher) Publish(ctx context.Context, event Event) error {
	if p == nil || p.client == nil {
		return errors.New("redis publisher not configured")
	}
	target := p.stream
	if strings.TrimSpace(target) == "" {
		target = strings.TrimSpace(event.Topic)
	}
	if target == "" {
		return fmt.Errorf("redis stream missing")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}
	args := &redis.XAddArgs{
		Stream: target,
		Values: map[string]any{"payload": payload},
	}
	if p.maxLen > 0 {
		args.MaxLen = p.maxLen
		args.Approx = true
	}
	if err := p.client.XAdd(ctx, args).Err(); err != nil {
		if p.logger != nil {
			p.logger.Warn("failed to publish event", "stream", target, "error", err)
		}
		return fmt.Errorf("publish to redis stream: %w", err)
	}
	return nil
}

// KafkaStubPublisher simulates a Kafka publisher for future expansion.
type KafkaStubPublisher struct {
	topic  string
	logger *slog.Logger
}

// NewKafkaStubPublisher builds a stub that simply logs publish attempts.
func NewKafkaStubPublisher(topic string, logger *slog.Logger) *KafkaStubPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &KafkaStubPublisher{topic: strings.TrimSpace(topic), logger: logger.With("component", "kafka_stub")}
}

// Publish logs the event instead of sending it to Kafka.
func (k *KafkaStubPublisher) Publish(ctx context.Context, event Event) error {
	if k == nil || k.logger == nil {
		return nil
	}
	topic := k.topic
	if topic == "" {
		topic = strings.TrimSpace(event.Topic)
	}
	k.logger.Debug("kafka stub publish", "topic", topic, "action", event.Payload["action"], "tenant_id", event.Payload["tenant_id"])
	return nil
}
