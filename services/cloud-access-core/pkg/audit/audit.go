package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"gorm.io/datatypes"
)

// Entry captures contextual details for an auditable action.
type Entry struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
	Action   string
	Result   string
	Geo      string
	Latency  time.Duration
	Metadata map[string]any
}

// Recorder persists audit entries for compliance/troubleshooting.
type Recorder interface {
	Record(ctx context.Context, entry Entry)
}

// DBRecorder writes audit entries to PostgreSQL via the repository.
type DBRecorder struct {
	repo   *gormdb.Repository
	logger *slog.Logger
}

// NewDBRecorder constructs a Recorder backed by PostgreSQL.
func NewDBRecorder(repo *gormdb.Repository, logger *slog.Logger) Recorder {
	if logger == nil {
		logger = slog.Default()
	}
	return &DBRecorder{repo: repo, logger: logger.With("component", "audit_recorder")}
}

// Record persists the entry asynchronously best-effort.
func (r *DBRecorder) Record(ctx context.Context, entry Entry) {
	if r == nil || r.repo == nil {
		return
	}
	metadata := datatypes.JSONMap{}
	for k, v := range entry.Metadata {
		metadata[k] = v
	}
	audit := &gormdb.AuditLog{
		EventID:   uuid.New(),
		TenantID:  entry.TenantID,
		ActorID:   entry.ActorID,
		Action:    entry.Action,
		Result:    entry.Result,
		LatencyMs: int(entry.Latency.Milliseconds()),
		Geo:       entry.Geo,
		Metadata:  metadata,
	}
	if err := r.repo.RecordAuditLog(ctx, audit); err != nil && r.logger != nil {
		r.logger.Warn("failed to persist audit log", "error", err, "action", entry.Action)
	}
}
