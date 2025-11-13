package usage

import (
	"context"
	"errors"
	"log/slog"
	"time"

	cloudEvents "github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/metrics"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/sessions"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/storage/gormdb"
)

// buffer abstracts the durable queue used when redis is unavailable.
type buffer interface {
	Start(ctx context.Context, publisher cloudEvents.Publisher) error
	Enqueue(event cloudEvents.Event) error
	Pending() int64
	Flush(ctx context.Context) error
	Close() error
}

// Exporter persists aggregated usage rows and mirrors them to Redis.
type Exporter struct {
	repo      *sessions.Repository
	publisher cloudEvents.Publisher
	buffer    buffer
	metrics   *metrics.Collector
	stream    string
	logger    *slog.Logger
}

// NewExporter wires repository + publisher dependencies.
func NewExporter(repo *sessions.Repository, publisher cloudEvents.Publisher, buf buffer, stream string, metrics *metrics.Collector, logger *slog.Logger) *Exporter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Exporter{
		repo:      repo,
		publisher: publisher,
		buffer:    buf,
		metrics:   metrics,
		stream:    stream,
		logger:    logger.With("component", "usage.exporter"),
	}
}

// Export writes usage rows to Postgres and mirrors them to Redis.
func (e *Exporter) Export(ctx context.Context, records []Record) error {
	if e == nil || len(records) == 0 {
		return nil
	}
	for _, record := range records {
		usageRow := &gormdb.SessionUsage{
			SessionID:     record.SessionID,
			TenantID:      record.TenantID,
			SpeechSeconds: record.SpeechSeconds,
			LLMTokens:     record.LLMTokens,
			ErrorCount:    record.ErrorCount,
			WindowStart:   record.WindowStart,
			WindowEnd:     record.WindowEnd,
		}
		if err := e.repo.UpsertUsageWindow(ctx, usageRow); err != nil {
			return err
		}
		e.emit(ctx, record)
	}
	return nil
}

// StartBuffer boots the durable buffer flush loop when present.
func (e *Exporter) StartBuffer(ctx context.Context) error {
	if e == nil || e.buffer == nil || e.publisher == nil {
		return nil
	}
	if err := e.buffer.Start(ctx, e.publisher); err != nil {
		return err
	}
	if e.metrics != nil {
		e.metrics.SetUsageBuffered(e.buffer.Pending())
	}
	return nil
}

// CloseBuffer stops the buffer if configured.
func (e *Exporter) CloseBuffer() error {
	if e == nil || e.buffer == nil {
		return nil
	}
	return e.buffer.Close()
}

func (e *Exporter) emit(ctx context.Context, record Record) {
	if e.publisher == nil {
		return
	}
	timestamp := record.WindowEnd
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	payload := map[string]any{
		"action":         "usage.window",
		"session_id":     record.SessionID.String(),
		"tenant_id":      record.TenantID.String(),
		"speech_seconds": record.SpeechSeconds,
		"llm_tokens":     record.LLMTokens,
		"error_count":    record.ErrorCount,
		"window_start":   record.WindowStart.UTC().Format(time.RFC3339Nano),
		"window_end":     record.WindowEnd.UTC().Format(time.RFC3339Nano),
	}
	event := cloudEvents.Event{Topic: e.stream, Payload: payload, Timestamp: timestamp}
	if err := e.publisher.Publish(ctx, event); err != nil {
		e.handlePublishFailure(event, err)
	}
}

func (e *Exporter) handlePublishFailure(event cloudEvents.Event, publishErr error) {
	if e.logger != nil {
		e.logger.Warn("usage stream publish failed", "error", publishErr)
	}
	if e.buffer == nil {
		return
	}
	if err := e.buffer.Enqueue(event); err != nil && e.logger != nil {
		e.logger.Error("usage buffer enqueue failed", "error", err)
	}
	if e.metrics != nil {
		e.metrics.SetUsageBuffered(e.buffer.Pending())
	}
}

// FlushBuffer exposes buffer flush for tests.
func (e *Exporter) FlushBuffer(ctx context.Context) error {
	if e == nil || e.buffer == nil {
		return nil
	}
	if err := e.buffer.Flush(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	if e.metrics != nil {
		e.metrics.SetUsageBuffered(e.buffer.Pending())
	}
	return nil
}
