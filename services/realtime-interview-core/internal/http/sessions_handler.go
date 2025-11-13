package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/authn"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/sessions"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/storage/gormdb"
)

// SessionService defines the subset of methods required by the HTTP layer.
type SessionService interface {
	CreateSession(ctx context.Context, req sessions.CreateRequest) (*gormdb.Session, error)
	TransitionSession(ctx context.Context, sessionID uuid.UUID, req sessions.TransitionRequest) (*gormdb.Session, error)
	Participants(ctx context.Context, sessionID uuid.UUID) ([]gormdb.SessionParticipant, error)
	Session(ctx context.Context, sessionID uuid.UUID) (*gormdb.Session, error)
}

// SessionsHandler exposes REST endpoints for session lifecycle management.
type SessionsHandler struct {
	service SessionService
	logger  *slog.Logger
}

// NewSessionsHandler constructs a handler instance.
func NewSessionsHandler(service SessionService, logger *slog.Logger) *SessionsHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SessionsHandler{service: service, logger: logger.With("component", "sessions_http")}
}

// Register attaches routes to the provided router.
func (h *SessionsHandler) Register(router chi.Router) {
	router.Post("/sessions", h.wrap(h.handleCreate))
	router.Get("/sessions/{sessionID}", h.wrap(h.handleGet))
	router.Post("/sessions/{sessionID}/transition", h.wrap(h.handleTransition))
	router.Get("/sessions/{sessionID}/participants", h.wrap(h.handleParticipants))
}

type handlerFunc func(http.ResponseWriter, *http.Request) error

func (h *SessionsHandler) wrap(fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			h.writeError(w, r, err)
		}
	}
}

func (h *SessionsHandler) handleCreate(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	claims, err := h.claims(r)
	if err != nil {
		return err
	}
	var req createSessionRequest
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	tenantID := parseUUID(claims.TenantID)
	userID := parseUUID(claims.Subject)
	if tenantID == uuid.Nil || userID == uuid.Nil {
		return errBadRequest
	}
	createReq := sessions.CreateRequest{
		TenantID:   tenantID,
		UserID:     userID,
		Mode:       req.Mode,
		RequestKey: req.RequestKey,
		Metadata:   req.Metadata,
	}
	if id := parseUUID(req.DeviceID); id != uuid.Nil {
		createReq.DeviceID = id
	}
	for _, p := range req.Participants {
		uid := parseUUID(p.UserID)
		if uid == uuid.Nil {
			continue
		}
		createReq.Participants = append(createReq.Participants, sessions.ParticipantInput{UserID: uid, Role: p.Role})
	}
	session, err := h.service.CreateSession(r.Context(), createReq)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, sessionResponseFromModel(session))
}

func (h *SessionsHandler) handleGet(w http.ResponseWriter, r *http.Request) error {
	claims, err := h.claims(r)
	if err != nil {
		return err
	}
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		return errBadRequest
	}
	session, err := h.service.Session(r.Context(), sessionID)
	if err != nil {
		return err
	}
	if tenantID := parseUUID(claims.TenantID); tenantID != uuid.Nil && session.TenantID != tenantID {
		return errors.New("forbidden")
	}
	return writeJSON(w, http.StatusOK, sessionResponseFromModel(session))
}

func (h *SessionsHandler) handleTransition(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	if _, err := h.claims(r); err != nil {
		return err
	}
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		return errBadRequest
	}
	var req transitionRequest
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	transitionReq := sessions.TransitionRequest{NextState: req.NextState, MetadataPatch: req.MetadataPatch}
	if req.EndedAt != "" {
		if ts, parseErr := time.Parse(time.RFC3339Nano, req.EndedAt); parseErr == nil {
			transitionReq.EndedAt = &ts
		}
	}
	session, err := h.service.TransitionSession(r.Context(), sessionID, transitionReq)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, sessionResponseFromModel(session))
}

func (h *SessionsHandler) handleParticipants(w http.ResponseWriter, r *http.Request) error {
	if _, err := h.claims(r); err != nil {
		return err
	}
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		return errBadRequest
	}
	participants, err := h.service.Participants(r.Context(), sessionID)
	if err != nil {
		return err
	}
	resp := make([]participantResponse, 0, len(participants))
	for _, participant := range participants {
		resp = append(resp, participantResponse{
			ID:        participant.ID.String(),
			SessionID: participant.SessionID.String(),
			UserID:    participant.UserID.String(),
			Role:      participant.Role,
			JoinedAt:  participant.JoinedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return writeJSON(w, http.StatusOK, map[string]any{"participants": resp})
}

func (h *SessionsHandler) claims(r *http.Request) (*crypto.Claims, error) {
	claims, ok := authn.ClaimsFromContext(r.Context())
	if !ok {
		return nil, errors.New("unauthorized")
	}
	return claims, nil
}

func (h *SessionsHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code := "50000"
	message := "internal error"
	switch {
	case errors.Is(err, errBadRequest), errors.Is(err, sessions.ErrInvalidMode):
		status = http.StatusBadRequest
		code = "40001"
		message = "invalid request"
	case errors.Is(err, sessions.ErrSessionNotFound):
		status = http.StatusNotFound
		code = "40401"
		message = "session not found"
	case errors.Is(err, sessions.ErrInvalidTransition), errors.Is(err, sessions.ErrSessionStateConflict):
		status = http.StatusConflict
		code = "40901"
		message = "invalid transition"
	case errors.Is(err, sessions.ErrSessionExists):
		status = http.StatusConflict
		code = "40902"
		message = "session exists"
	case strings.EqualFold(err.Error(), "unauthorized"):
		status = http.StatusUnauthorized
		code = "40101"
		message = "unauthorized"
	case strings.EqualFold(err.Error(), "forbidden"):
		status = http.StatusForbidden
		code = "40301"
		message = "forbidden"
	default:
		h.logger.Error("session handler error", "error", err)
	}
	_ = writeJSON(w, status, map[string]any{
		"code":       code,
		"message":    message,
		"request_id": r.Header.Get("X-Request-ID"),
	})
}

type createSessionRequest struct {
	Mode         string                   `json:"mode"`
	RequestKey   string                   `json:"request_key"`
	DeviceID     string                   `json:"device_id"`
	Metadata     map[string]any           `json:"metadata"`
	Participants []participantRequestBody `json:"participants"`
}

type participantRequestBody struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type transitionRequest struct {
	NextState     string         `json:"next_state"`
	MetadataPatch map[string]any `json:"metadata_patch"`
	EndedAt       string         `json:"ended_at"`
}

type sessionResponse struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenant_id"`
	UserID        string         `json:"user_id"`
	State         string         `json:"state"`
	Mode          string         `json:"mode"`
	RequestKey    string         `json:"request_key"`
	Metadata      map[string]any `json:"metadata"`
	StartedAt     string         `json:"started_at"`
	UpdatedAt     string         `json:"updated_at"`
	EndedAt       *string        `json:"ended_at,omitempty"`
	LastHeartbeat string         `json:"last_heartbeat"`
}

type participantResponse struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	JoinedAt  string `json:"joined_at"`
}

func sessionResponseFromModel(session *gormdb.Session) sessionResponse {
	metadata := map[string]any{}
	if session.Metadata != nil {
		for k, v := range session.Metadata {
			metadata[k] = v
		}
	}
	resp := sessionResponse{
		ID:            session.ID.String(),
		TenantID:      session.TenantID.String(),
		UserID:        session.UserID.String(),
		State:         session.State,
		Mode:          session.Mode,
		RequestKey:    session.RequestKey,
		Metadata:      metadata,
		StartedAt:     session.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:     session.UpdatedAt.UTC().Format(time.RFC3339Nano),
		LastHeartbeat: session.LastHeartbeat.UTC().Format(time.RFC3339Nano),
	}
	if session.EndedAt != nil {
		ended := session.EndedAt.UTC().Format(time.RFC3339Nano)
		resp.EndedAt = &ended
	}
	return resp
}

func parseUUID(value string) uuid.UUID {
	value = strings.TrimSpace(value)
	if value == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil
	}
	return id
}
