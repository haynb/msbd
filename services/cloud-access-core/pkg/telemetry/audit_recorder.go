package telemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/audit"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"gorm.io/datatypes"
)

// auditWriter captures the repository capability required for persisting audit logs.
type auditWriter interface {
	RecordAuditLog(ctx context.Context, logEntry *gormdb.AuditLog) error
}

// eventBuffer represents the buffering capability for failed publishes.
type eventBuffer interface {
	Enqueue(event events.Event) error
	Pending() int64
}

// AuditRecorderConfig configures the recorder behaviour.
type AuditRecorderConfig struct {
	Topic string
}

func (c AuditRecorderConfig) topic() string {
	if c.Topic == "" {
		return "audit.events"
	}
	return c.Topic
}

// AuditRecorder implements audit.Recorder and fans out to database + event sinks.
type AuditRecorder struct {
	writer    auditWriter
	publisher events.Publisher
	buffer    eventBuffer
	metrics   *Metrics
	logger    *slog.Logger
	cfg       AuditRecorderConfig
}

// NewAuditRecorder builds a Recorder that writes to Postgres and mirrors events to downstream pipelines.
func NewAuditRecorder(writer auditWriter, publisher events.Publisher, buffer eventBuffer, metrics *Metrics, logger *slog.Logger, cfg AuditRecorderConfig) *AuditRecorder {
	if logger == nil {
		logger = slog.Default()
	}
	return &AuditRecorder{
		writer:    writer,
		publisher: publisher,
		buffer:    buffer,
		metrics:   metrics,
		logger:    logger.With("component", "audit_recorder"),
		cfg:       cfg,
	}
}

// Record persists the audit entry and mirrors it asynchronously.
func (r *AuditRecorder) Record(ctx context.Context, entry audit.Entry) {
	if r == nil || r.writer == nil {
		return
	}
	metadata := datatypes.JSONMap{}
	for k, v := range entry.Metadata {
		metadata[k] = v
	}
	logEntry := &gormdb.AuditLog{
		BaseModel: gormdb.BaseModel{ID: uuid.New()},
		EventID:   uuid.New(),
		TenantID:  entry.TenantID,
		ActorID:   entry.ActorID,
		Action:    entry.Action,
		Result:    entry.Result,
		LatencyMs: int(entry.Latency.Milliseconds()),
		Geo:       entry.Geo,
		Metadata:  metadata,
	}
	if err := r.writer.RecordAuditLog(ctx, logEntry); err != nil {
		if r.logger != nil {
			r.logger.Warn("failed to persist audit log", "error", err, "action", entry.Action)
		}
		if r.metrics != nil {
			r.metrics.RecordAuditFailure("database")
		}
		return
	}
	if r.metrics != nil {
		r.metrics.ObserveAudit(entry.Action, entry.Result, entry.Latency)
	}
	if r.publisher == nil {
		return
	}
	event := events.Event{
		Topic:     r.cfg.topic(),
		Timestamp: time.Now().UTC(),
		Payload: map[string]any{
			"event_id":   logEntry.EventID.String(),
			"tenant_id":  logEntry.TenantID.String(),
			"actor_id":   logEntry.ActorID.String(),
			"action":     logEntry.Action,
			"result":     logEntry.Result,
			"geo":        logEntry.Geo,
			"latency_ms": logEntry.LatencyMs,
			"metadata":   metadata,
			"event_type": "audit.recorded",
		},
	}
	if err := r.publisher.Publish(ctx, event); err != nil {
		r.handlePublishFailure(event, err)
	}
}

func (r *AuditRecorder) handlePublishFailure(event events.Event, publishErr error) {
	if r.logger != nil {
		r.logger.Warn("failed to publish audit event", "error", publishErr)
	}
	if r.metrics != nil {
		r.metrics.RecordAuditFailure("publisher")
	}
	if r.buffer == nil {
		return
	}
	if err := r.buffer.Enqueue(event); err != nil {
		if r.logger != nil {
			r.logger.Error("failed to enqueue audit event", "error", err)
		}
		if r.metrics != nil {
			r.metrics.RecordAuditFailure("buffer")
		}
		return
	}
}
