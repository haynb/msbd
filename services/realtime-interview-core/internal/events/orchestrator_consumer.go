package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	cloudEvents "github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/orchestrator"
)

// OrchestratorConsumer subscribes to transcript events and forwards them to the orchestrator service.
type OrchestratorConsumer struct {
	bootstrap.BaseModule

	redis    *redis.Client
	stream   string
	group    string
	consumer string
	service  *orchestrator.Service
	logger   *slog.Logger

	cancel context.CancelFunc
	once   sync.Once
	done   chan struct{}
}

// NewOrchestratorConsumer builds a background module that consumes transcript events.
func NewOrchestratorConsumer(redisClient *redis.Client, stream string, service *orchestrator.Service, logger *slog.Logger) *OrchestratorConsumer {
	if redisClient == nil || service == nil || strings.TrimSpace(stream) == "" {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &OrchestratorConsumer{
		redis:    redisClient,
		stream:   strings.TrimSpace(stream),
		group:    "orchestrator-workers",
		consumer: fmt.Sprintf("orch-%d", time.Now().UnixNano()),
		service:  service,
		logger:   logger.With("module", "orchestrator_consumer"),
		done:     make(chan struct{}),
	}
}

// Name implements bootstrap.Module.
func (c *OrchestratorConsumer) Name() string { return "orchestrator-consumer" }

// RegisterRoutes implements bootstrap.Module (no HTTP handlers yet).
func (c *OrchestratorConsumer) RegisterRoutes(mux *http.ServeMux) error { return nil }

// Start begins consuming Redis Stream entries.
func (c *OrchestratorConsumer) Start(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if err := c.ensureGroup(ctx); err != nil {
		return err
	}
	loopCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.consume(loopCtx)
	return nil
}

// Shutdown stops the consumer loop.
func (c *OrchestratorConsumer) Shutdown(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.once.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
	})
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *OrchestratorConsumer) ensureGroup(ctx context.Context) error {
	if c.redis == nil {
		return errors.New("redis client required")
	}
	if err := c.redis.XGroupCreateMkStream(ctx, c.stream, c.group, "$ ").Err(); err != nil {
		if !strings.Contains(err.Error(), "BUSYGROUP") {
			return fmt.Errorf("create consumer group: %w", err)
		}
	}
	return nil
}

func (c *OrchestratorConsumer) consume(ctx context.Context) {
	defer close(c.done)
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		res, err := c.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    c.group,
			Consumer: c.consumer,
			Streams:  []string{c.stream, ">"},
			Count:    10,
			Block:    3 * time.Second,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			c.logger.Warn("stream read failed", "error", err)
			select {
			case <-time.After(backoff):
				if backoff < 10*time.Second {
					backoff *= 2
				}
				continue
			case <-ctx.Done():
				return
			}
		}
		backoff = time.Second
		for _, stream := range res {
			for _, msg := range stream.Messages {
				if err := c.handleMessage(ctx, msg); err != nil {
					c.logger.Error("handle transcript message failed", "error", err, "id", msg.ID)
				}
				if err := c.redis.XAck(ctx, c.stream, c.group, msg.ID).Err(); err != nil {
					c.logger.Warn("ack failed", "error", err, "id", msg.ID)
				}
			}
		}
	}
}

func (c *OrchestratorConsumer) handleMessage(ctx context.Context, msg redis.XMessage) error {
	raw, ok := msg.Values["payload"].(string)
	if !ok {
		return errors.New("missing payload field")
	}
	var evt cloudEvents.Event
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if evt.Payload == nil {
		return errors.New("empty payload")
	}
	if action, _ := evt.Payload["action"].(string); action != "transcript.final" {
		return nil
	}
	payload, err := buildSegment(evt.Payload)
	if err != nil {
		return err
	}
	return c.service.ProcessSegment(ctx, payload)
}

func buildSegment(data map[string]any) (orchestrator.SegmentPayload, error) {
	sessionID, err := parseUUID(data["session_id"])
	if err != nil {
		return orchestrator.SegmentPayload{}, err
	}
	tenantID, _ := parseUUID(data["tenant_id"])
	userID, _ := parseUUID(data["user_id"])
	sequence := parseInt64(data["sequence"])
	confidence := float32(parseFloat(data["confidence"]))
	provider, _ := data["provider"].(string)
	text, _ := data["text"].(string)
	createdAt := parseTime(data["created_at"]).UTC()
	metadata := map[string]any{}
	if rawMeta, ok := data["metadata"].(map[string]any); ok {
		metadata = rawMeta
	}
	return orchestrator.SegmentPayload{
		SessionID:  sessionID,
		TenantID:   tenantID,
		UserID:     userID,
		Sequence:   sequence,
		Text:       text,
		Provider:   provider,
		Confidence: confidence,
		Metadata:   metadata,
		Final:      true,
		CreatedAt:  createdAt,
	}, nil
}

func parseUUID(value any) (uuid.UUID, error) {
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return uuid.Nil, nil
		}
		return uuid.Parse(v)
	default:
		return uuid.Nil, nil
	}
}

func parseInt64(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case int:
		return int64(v)
	case int64:
		return v
	case json.Number:
		i, _ := v.Int64()
		return i
	case string:
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func parseFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func parseTime(value any) time.Time {
	switch v := value.(type) {
	case string:
		if ts, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return ts
		}
	}
	return time.Now().UTC()
}
