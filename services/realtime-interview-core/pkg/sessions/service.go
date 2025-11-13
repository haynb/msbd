package sessions

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	cloudEvents "github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/storage/gormdb"
)

var (
	// ErrInvalidMode is returned when an unsupported session mode is supplied.
	ErrInvalidMode = errors.New("invalid session mode")
	// ErrInvalidTransition indicates the requested state change is not allowed.
	ErrInvalidTransition = errors.New("invalid session transition")
	// ErrSessionExists is returned when an idempotent create request already exists.
	ErrSessionExists = errors.New("session already exists")
)

// ParticipantInput defines the payload for creating participants.
type ParticipantInput struct {
	UserID uuid.UUID
	Role   string
}

// CreateRequest captures session creation parameters.
type CreateRequest struct {
	TenantID     uuid.UUID
	UserID       uuid.UUID
	DeviceID     uuid.UUID
	Mode         string
	RequestKey   string
	Metadata     map[string]any
	Participants []ParticipantInput
}

// TransitionRequest describes a requested state update.
type TransitionRequest struct {
	NextState     string
	MetadataPatch map[string]any
	EndedAt       *time.Time
}

// Service coordinates business logic around the repository and event fan-out.
type Service struct {
	repo      *Repository
	publisher cloudEvents.Publisher
	logger    *slog.Logger
	now       func() time.Time
}

// NewService constructs a session service.
func NewService(repo *Repository, publisher cloudEvents.Publisher, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repo: repo, publisher: publisher, logger: logger.With("component", "sessions.service"), now: time.Now}
}

// SetClock overrides the clock (tests only).
func (s *Service) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// CreateSession persisted a new session row plus optional participants.
func (s *Service) CreateSession(ctx context.Context, req CreateRequest) (*gormdb.Session, error) {
	if req.TenantID == uuid.Nil || req.UserID == uuid.Nil {
		return nil, ErrSessionNotFound
	}
	mode, err := normalizeMode(req.Mode)
	if err != nil {
		return nil, err
	}
	session := &gormdb.Session{
		TenantID:   req.TenantID,
		UserID:     req.UserID,
		RequestKey: strings.TrimSpace(req.RequestKey),
		Mode:       mode,
		Metadata:   datatypes.JSONMap{},
	}
	if req.DeviceID != uuid.Nil {
		session.DeviceID = &req.DeviceID
	}
	if len(req.Metadata) > 0 {
		session.Metadata = datatypes.JSONMap(req.Metadata)
	}
	if err := s.repo.CreateSession(ctx, session); err != nil {
		if gormdb.IsUniqueViolation(err) && session.RequestKey != "" {
			existing, lookupErr := s.repo.SessionByRequestKey(ctx, req.TenantID, session.RequestKey)
			if lookupErr == nil {
				return existing, ErrSessionExists
			}
		}
		return nil, err
	}
	participants := req.Participants
	if len(participants) == 0 {
		participants = append(participants, ParticipantInput{UserID: req.UserID, Role: "interviewer"})
	}
	for _, p := range participants {
		if p.UserID == uuid.Nil {
			continue
		}
		role := normalizeRole(p.Role)
		participant := &gormdb.SessionParticipant{SessionID: session.ID, UserID: p.UserID, Role: role}
		if err := s.repo.AddParticipant(ctx, participant); err != nil {
			s.logger.Warn("add participant failed", "error", err, "session", session.ID)
		}
	}
	s.publishEvent(ctx, "session.created", session)
	return session, nil
}

// TransitionSession validates state transitions and publishes lifecycle events.
func (s *Service) TransitionSession(ctx context.Context, sessionID uuid.UUID, req TransitionRequest) (*gormdb.Session, error) {
	if sessionID == uuid.Nil {
		return nil, ErrSessionNotFound
	}
	next := strings.ToLower(strings.TrimSpace(req.NextState))
	if next == "" {
		return nil, ErrInvalidTransition
	}
	current, err := s.repo.SessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if !allowTransition(current.State, next) {
		return nil, ErrInvalidTransition
	}
	options := TransitionOptions{ExpectedState: current.State, NextState: next}
	if len(req.MetadataPatch) > 0 {
		options.MetadataPatch = datatypes.JSONMap(req.MetadataPatch)
	}
	if strings.EqualFold(next, "ended") {
		ended := s.now().UTC()
		if req.EndedAt != nil {
			ended = req.EndedAt.UTC()
		}
		options.EndedAt = &ended
	}
	updated, err := s.repo.TransitionSession(ctx, sessionID, options)
	if err != nil {
		return nil, err
	}
	s.publishEvent(ctx, "session.transition", updated)
	return updated, nil
}

// Participants returns all participants associated with the session.
func (s *Service) Participants(ctx context.Context, sessionID uuid.UUID) ([]gormdb.SessionParticipant, error) {
	if sessionID == uuid.Nil {
		return nil, ErrSessionNotFound
	}
	if _, err := s.repo.SessionByID(ctx, sessionID); err != nil {
		return nil, err
	}
	return s.repo.ParticipantsBySession(ctx, sessionID)
}

// Session returns the session by ID.
func (s *Service) Session(ctx context.Context, sessionID uuid.UUID) (*gormdb.Session, error) {
	return s.repo.SessionByID(ctx, sessionID)
}

func (s *Service) publishEvent(ctx context.Context, action string, session *gormdb.Session) {
	if s.publisher == nil || session == nil {
		return
	}
	metadata := map[string]any{}
	if session.Metadata != nil {
		for k, v := range session.Metadata {
			metadata[k] = v
		}
	}
	payload := map[string]any{
		"action":         action,
		"session_id":     session.ID.String(),
		"tenant_id":      session.TenantID.String(),
		"user_id":        session.UserID.String(),
		"state":          session.State,
		"mode":           session.Mode,
		"request_key":    session.RequestKey,
		"last_heartbeat": session.LastHeartbeat.UTC().Format(time.RFC3339Nano),
		"metadata":       metadata,
		"updated_at":     session.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if session.EndedAt != nil {
		payload["ended_at"] = session.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	event := cloudEvents.Event{Topic: action, Payload: payload, Timestamp: s.now().UTC()}
	if err := s.publisher.Publish(ctx, event); err != nil {
		s.logger.Warn("session event publish failed", "error", err, "session", session.ID)
	}
}

func normalizeMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "interview", nil
	}
	switch mode {
	case "interview", "diagnostic":
		return mode, nil
	default:
		return "", ErrInvalidMode
	}
}

func normalizeRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "interviewer", "assistant", "observer":
		return role
	default:
		return "observer"
	}
}

func allowTransition(current, next string) bool {
	current = strings.ToLower(strings.TrimSpace(current))
	next = strings.ToLower(strings.TrimSpace(next))
	if current == next {
		return true
	}
	switch current {
	case "active":
		return next == "paused" || next == "ended"
	case "paused":
		return next == "active" || next == "ended"
	case "ended":
		return false
	default:
		return false
	}
}
