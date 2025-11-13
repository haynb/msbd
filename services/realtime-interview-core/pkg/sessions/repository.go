package sessions

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/storage/gormdb"
)

var (
	// ErrSessionNotFound is returned when the target session does not exist.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionStateConflict indicates the session state does not match the expected state.
	ErrSessionStateConflict = errors.New("session state conflict")
	// ErrParticipantNotFound indicates the participant row is missing.
	ErrParticipantNotFound = errors.New("participant not found")
)

// Repository coordinates all session persistence operations.
type Repository struct {
	db *gorm.DB
}

// NewRepository returns a session repository bound to db.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// CreateSession saves a new session row.
func (r *Repository) CreateSession(ctx context.Context, session *gormdb.Session) error {
	if session == nil {
		return errors.New("session is required")
	}
	if session.Metadata == nil {
		session.Metadata = datatypes.JSONMap{}
	}
	if session.LastHeartbeat.IsZero() {
		session.LastHeartbeat = time.Now().UTC()
	}
	if session.State == "" {
		session.State = "active"
	}
	return r.db.WithContext(ctx).Create(session).Error
}

// SessionByID fetches a session by ID.
func (r *Repository) SessionByID(ctx context.Context, id uuid.UUID) (*gormdb.Session, error) {
	var session gormdb.Session
	err := r.db.WithContext(ctx).First(&session, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// SessionByRequestKey fetches a session scoped by tenant + request key.
func (r *Repository) SessionByRequestKey(ctx context.Context, tenantID uuid.UUID, requestKey string) (*gormdb.Session, error) {
	requestKey = strings.TrimSpace(requestKey)
	if tenantID == uuid.Nil || requestKey == "" {
		return nil, ErrSessionNotFound
	}
	var session gormdb.Session
	err := r.db.WithContext(ctx).
		First(&session, "tenant_id = ? AND request_key = ?", tenantID, requestKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// TransitionOptions describes a state transition payload.
type TransitionOptions struct {
	ExpectedState string
	NextState     string
	EndedAt       *time.Time
	HeartbeatAt   time.Time
	MetadataPatch datatypes.JSONMap
}

// TransitionSession atomically updates session state with optimistic locking.
func (r *Repository) TransitionSession(ctx context.Context, sessionID uuid.UUID, opts TransitionOptions) (*gormdb.Session, error) {
	if opts.NextState == "" {
		return nil, errors.New("next state required")
	}
	var updated gormdb.Session
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lock := tx.Clauses(clause.Locking{Strength: "UPDATE"})
		if err := lock.First(&updated, "id = ?", sessionID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSessionNotFound
			}
			return err
		}
		if opts.ExpectedState != "" && !strings.EqualFold(updated.State, opts.ExpectedState) {
			return ErrSessionStateConflict
		}
		updated.State = opts.NextState
		if opts.EndedAt != nil {
			updated.EndedAt = opts.EndedAt
		}
		if !opts.HeartbeatAt.IsZero() {
			updated.LastHeartbeat = opts.HeartbeatAt
		}
		if opts.MetadataPatch != nil {
			if updated.Metadata == nil {
				updated.Metadata = datatypes.JSONMap{}
			}
			for k, v := range opts.MetadataPatch {
				if v == nil {
					delete(updated.Metadata, k)
					continue
				}
				updated.Metadata[k] = v
			}
		}
		return tx.Save(&updated).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// UpdateHeartbeat refreshes the session heartbeat timestamp.
func (r *Repository) UpdateHeartbeat(ctx context.Context, sessionID uuid.UUID, heartbeat time.Time) error {
	if heartbeat.IsZero() {
		heartbeat = time.Now().UTC()
	}
	result := r.db.WithContext(ctx).Model(&gormdb.Session{}).
		Where("id = ?", sessionID).
		Update("last_heartbeat", heartbeat)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// AddParticipant inserts a participant record idempotently.
func (r *Repository) AddParticipant(ctx context.Context, participant *gormdb.SessionParticipant) error {
	if participant == nil {
		return errors.New("participant required")
	}
	if participant.JoinedAt.IsZero() {
		participant.JoinedAt = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}, {Name: "user_id"}, {Name: "role"}},
		DoNothing: true,
	}).Create(participant).Error
}

// MarkParticipantLeft updates the leave timestamp.
func (r *Repository) MarkParticipantLeft(ctx context.Context, sessionID, userID uuid.UUID, role string, leftAt time.Time) error {
	if leftAt.IsZero() {
		leftAt = time.Now().UTC()
	}
	result := r.db.WithContext(ctx).Model(&gormdb.SessionParticipant{}).
		Where("session_id = ? AND user_id = ? AND role = ?", sessionID, userID, role).
		Updates(map[string]any{
			"left_at":    leftAt,
			"updated_at": gorm.Expr("GREATEST(updated_at, ?)", leftAt),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrParticipantNotFound
	}
	return nil
}

// ParticipantsBySession returns all participants for a session.
func (r *Repository) ParticipantsBySession(ctx context.Context, sessionID uuid.UUID) ([]gormdb.SessionParticipant, error) {
	participants := make([]gormdb.SessionParticipant, 0)
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("joined_at ASC").
		Find(&participants).Error
	if err != nil {
		return nil, err
	}
	return participants, nil
}

// AppendSegment appends a transcript segment and auto-assigns sequence if missing.
func (r *Repository) AppendSegment(ctx context.Context, segment *gormdb.SessionSegment) error {
	if segment == nil {
		return errors.New("segment required")
	}
	if segment.Transcript == nil {
		segment.Transcript = datatypes.JSONMap{}
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if segment.Sequence == 0 {
			var seq sql.NullInt64
			if err := tx.Model(&gormdb.SessionSegment{}).
				Where("session_id = ?", segment.SessionID).
				Select("COALESCE(MAX(sequence), 0)").
				Scan(&seq).Error; err != nil {
				return err
			}
			segment.Sequence = int(seq.Int64) + 1
		}
		return tx.Create(segment).Error
	})
}

// UpsertUsageWindow aggregates usage rows per session/window.
func (r *Repository) UpsertUsageWindow(ctx context.Context, usage *gormdb.SessionUsage) error {
	if usage == nil {
		return errors.New("usage required")
	}
	if usage.WindowStart.IsZero() || usage.WindowEnd.IsZero() {
		return errors.New("window start/end required")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "session_id"},
			{Name: "window_start"},
			{Name: "window_end"},
		},
		DoUpdates: clause.Assignments(map[string]any{
			"speech_seconds": gorm.Expr("session_usage.speech_seconds + EXCLUDED.speech_seconds"),
			"llm_tokens":     gorm.Expr("session_usage.llm_tokens + EXCLUDED.llm_tokens"),
			"error_count":    gorm.Expr("session_usage.error_count + EXCLUDED.error_count"),
			"updated_at":     gorm.Expr("GREATEST(session_usage.updated_at, EXCLUDED.updated_at)"),
		}),
	}).Create(usage).Error
}

// RecordProviderMetric stores provider-level telemetry.
func (r *Repository) RecordProviderMetric(ctx context.Context, metric *gormdb.SpeechProviderMetric) error {
	if metric == nil {
		return errors.New("metric required")
	}
	if metric.Provider == "" {
		return errors.New("provider required")
	}
	if metric.EventType == "" {
		return errors.New("event type required")
	}
	if metric.RecordedAt.IsZero() {
		metric.RecordedAt = time.Now().UTC()
	}
	if metric.Value == nil {
		metric.Value = datatypes.JSONMap{}
	}
	return r.db.WithContext(ctx).Create(metric).Error
}
