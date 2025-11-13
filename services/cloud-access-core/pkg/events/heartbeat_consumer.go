package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// HeartbeatEvent captures heartbeat metadata emitted by devices.
type HeartbeatEvent struct {
	DeviceID   string         `json:"device_id"`
	TenantID   string         `json:"tenant_id"`
	UserID     string         `json:"user_id"`
	Status     string         `json:"status"`
	Metrics    map[string]any `json:"metrics"`
	ReportedAt time.Time      `json:"reported_at"`
}

// HeartbeatPublisher emits heartbeat events into the configured backend.
type HeartbeatPublisher interface {
	Publish(ctx context.Context, event HeartbeatEvent) error
}

// RedisHeartbeatPublisher writes heartbeat events to a Redis Stream.
type RedisHeartbeatPublisher struct {
	client *redis.Client
	stream string
	logger *slog.Logger
}

// NewRedisHeartbeatPublisher builds a Redis-backed heartbeat publisher.
func NewRedisHeartbeatPublisher(client *redis.Client, stream string, logger *slog.Logger) *RedisHeartbeatPublisher {
	if logger == nil {
		logger = slog.Default()
	}
	if stream == "" {
		stream = "cloud-access-core:device-heartbeats"
	}
	return &RedisHeartbeatPublisher{client: client, stream: stream, logger: logger.With("component", "heartbeat_publisher")}
}

// Publish appends the event payload to the configured stream.
func (p *RedisHeartbeatPublisher) Publish(ctx context.Context, event HeartbeatEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}
	args := &redis.XAddArgs{
		Stream: p.stream,
		Values: map[string]any{"payload": payload},
	}
	if err := p.client.XAdd(ctx, args).Err(); err != nil {
		return fmt.Errorf("append heartbeat: %w", err)
	}
	return nil
}

// DeviceHeartbeatWriter persists heartbeat outcomes to storage.
type DeviceHeartbeatWriter interface {
	UpdateDeviceHeartbeat(ctx context.Context, deviceID uuid.UUID, status string, lastSeen time.Time, metadata map[string]any) error
}

// HeartbeatConsumerConfig powers the consumer lifecycle.
type HeartbeatConsumerConfig struct {
	Stream string
	Group  string
	// Consumer identifies this instance within the consumer group.
	Consumer string
	// Block defines how long each XREADGROUP call waits for new events.
	Block time.Duration
}

// HeartbeatConsumer consumes events from Redis Stream and updates storage.
type HeartbeatConsumer struct {
	client    *redis.Client
	writer    DeviceHeartbeatWriter
	cfg       HeartbeatConsumerConfig
	logger    *slog.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	startOnce sync.Once
}

// NewHeartbeatConsumer builds a consumer bound to the provided repository.
func NewHeartbeatConsumer(client *redis.Client, writer DeviceHeartbeatWriter, cfg HeartbeatConsumerConfig, logger *slog.Logger) *HeartbeatConsumer {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Stream == "" {
		cfg.Stream = "cloud-access-core:device-heartbeats"
	}
	if cfg.Group == "" {
		cfg.Group = "iam-heartbeat"
	}
	if cfg.Consumer == "" {
		cfg.Consumer = fmt.Sprintf("consumer-%d", time.Now().UnixNano())
	}
	if cfg.Block <= 0 {
		cfg.Block = 5 * time.Second
	}
	return &HeartbeatConsumer{
		client: client,
		writer: writer,
		cfg:    cfg,
		logger: logger.With("component", "heartbeat_consumer"),
	}
}

// Start begins streaming events.
func (c *HeartbeatConsumer) Start(ctx context.Context) error {
	if c.client == nil || c.writer == nil {
		return nil
	}
	var err error
	c.startOnce.Do(func() {
		err = c.ensureGroup(ctx)
		if err != nil {
			return
		}
		c.ctx, c.cancel = context.WithCancel(ctx)
		c.wg.Add(1)
		go c.loop()
	})
	return err
}

// Shutdown stops the consumer gracefully.
func (c *HeartbeatConsumer) Shutdown(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *HeartbeatConsumer) ensureGroup(ctx context.Context) error {
	if err := c.client.XGroupCreateMkStream(ctx, c.cfg.Stream, c.cfg.Group, "$").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create heartbeat group: %w", err)
	}
	return nil
}

func (c *HeartbeatConsumer) loop() {
	defer c.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}
		streams, err := c.client.XReadGroup(c.ctx, &redis.XReadGroupArgs{
			Group:    c.cfg.Group,
			Consumer: c.cfg.Consumer,
			Streams:  []string{c.cfg.Stream, ">"},
			Count:    50,
			Block:    c.cfg.Block,
		}).Result()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, redis.Nil) {
				continue
			}
			c.logger.Error("heartbeat read failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				if err := c.handleMessage(msg); err != nil {
					c.logger.Warn("failed to handle heartbeat", "error", err, "message_id", msg.ID)
				} else {
					if err := c.client.XAck(c.ctx, c.cfg.Stream, c.cfg.Group, msg.ID).Err(); err != nil {
						c.logger.Warn("failed to ack heartbeat", "error", err, "message_id", msg.ID)
					}
				}
			}
		}
	}
}

func (c *HeartbeatConsumer) handleMessage(msg redis.XMessage) error {
	payload, ok := msg.Values["payload"]
	if !ok {
		return errors.New("missing payload")
	}
	raw, ok := payload.(string)
	if !ok {
		if bytes, ok := payload.([]byte); ok {
			raw = string(bytes)
		} else {
			return errors.New("invalid payload type")
		}
	}
	var event HeartbeatEvent
	if err := json.Unmarshal([]byte(raw), &event); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	deviceID, err := uuid.Parse(event.DeviceID)
	if err != nil {
		return fmt.Errorf("invalid device_id: %w", err)
	}
	if event.ReportedAt.IsZero() {
		event.ReportedAt = time.Now().UTC()
	}
	return c.writer.UpdateDeviceHeartbeat(c.ctx, deviceID, event.Status, event.ReportedAt, event.Metrics)
}
