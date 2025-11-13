package unit

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/internal/http"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/authn"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/sessions"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/storage/gormdb"
)

func TestSessionsHandlerCreate(t *testing.T) {
	service := &fakeSessionService{}
	handler := httpapi.NewSessionsHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := chi.NewRouter()
	handler.Register(router)
	sessionID := uuid.New()
	tenantID := uuid.New()
	userID := uuid.New()
	now := time.Now().UTC()
	service.createResp = &gormdb.Session{
		BaseModel:     gormdb.BaseModel{ID: sessionID, CreatedAt: now, UpdatedAt: now},
		TenantID:      tenantID,
		UserID:        userID,
		State:         "active",
		Mode:          "interview",
		Metadata:      datatypes.JSONMap{"foo": "bar"},
		LastHeartbeat: now,
	}
	req := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"mode":"interview","participants":[{"user_id":"`+userID.String()+`","role":"assistant"}]}`))
	req = req.WithContext(authn.WithClaims(req.Context(), &crypto.Claims{TenantID: tenantID.String(), Subject: userID.String()}))
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusCreated, resp.Code)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &payload))
	require.Equal(t, sessionID.String(), payload["id"])
	require.Equal(t, tenantID.String(), service.lastCreate.TenantID.String())
	require.Len(t, service.lastCreate.Participants, 1)
	require.Equal(t, "assistant", service.lastCreate.Participants[0].Role)
}

func TestSessionsHandlerTransitionError(t *testing.T) {
	service := &fakeSessionService{}
	handler := httpapi.NewSessionsHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := chi.NewRouter()
	handler.Register(router)
	service.transitionErr = sessions.ErrInvalidTransition
	sessionID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessionID.String()+"/transition", strings.NewReader(`{"next_state":"ended"}`))
	req = req.WithContext(authn.WithClaims(req.Context(), &crypto.Claims{TenantID: uuid.New().String(), Subject: uuid.New().String()}))
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusConflict, resp.Code)
}

type fakeSessionService struct {
	lastCreate      sessions.CreateRequest
	createResp      *gormdb.Session
	createErr       error
	transitionResp  *gormdb.Session
	transitionErr   error
	participants    []gormdb.SessionParticipant
	participantsErr error
	sessionResp     *gormdb.Session
	sessionErr      error
}

func (f *fakeSessionService) CreateSession(ctx context.Context, req sessions.CreateRequest) (*gormdb.Session, error) {
	f.lastCreate = req
	return f.createResp, f.createErr
}

func (f *fakeSessionService) TransitionSession(ctx context.Context, sessionID uuid.UUID, req sessions.TransitionRequest) (*gormdb.Session, error) {
	return f.transitionResp, f.transitionErr
}

func (f *fakeSessionService) Participants(ctx context.Context, sessionID uuid.UUID) ([]gormdb.SessionParticipant, error) {
	return f.participants, f.participantsErr
}

func (f *fakeSessionService) Session(ctx context.Context, sessionID uuid.UUID) (*gormdb.Session, error) {
	return f.sessionResp, f.sessionErr
}
