package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/authn"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/sessions"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/storage/gormdb"
	realtimev1 "github.com/hayhandsome/msbd/services/realtime-interview-core/proto/realtime/v1"
)

// Server implements the realtime ControlService gRPC surface.
type Server struct {
	realtimev1.UnimplementedControlServiceServer
	service *sessions.Service
	logger  *slog.Logger
}

// NewServer builds a ControlServiceServer.
func NewServer(service *sessions.Service, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{service: service, logger: logger.With("component", "control.grpc")}
}

// CreateSession handles gRPC session creation requests.
func (s *Server) CreateSession(ctx context.Context, req *realtimev1.CreateSessionRequest) (*realtimev1.SessionRecord, error) {
	claims, err := s.claims(ctx)
	if err != nil {
		return nil, err
	}
	createReq := sessions.CreateRequest{
		TenantID:   parseUUID(claims.TenantID),
		UserID:     parseUUID(claims.Subject),
		Mode:       req.GetMode(),
		RequestKey: req.GetRequestKey(),
		Metadata:   mapFromStrings(req.GetMetadata()),
	}
	if id := parseUUID(req.GetDeviceId()); id != uuid.Nil {
		createReq.DeviceID = id
	}
	for _, participant := range req.GetParticipants() {
		uid := parseUUID(participant.GetUserId())
		if uid == uuid.Nil {
			continue
		}
		createReq.Participants = append(createReq.Participants, sessions.ParticipantInput{UserID: uid, Role: participant.GetRole()})
	}
	sess, err := s.service.CreateSession(ctx, createReq)
	if err != nil {
		return nil, s.translateError(err)
	}
	return toProtoSession(sess), nil
}

// GetSession fetches a session by ID.
func (s *Server) GetSession(ctx context.Context, req *realtimev1.SessionRequest) (*realtimev1.SessionRecord, error) {
	if _, err := s.claims(ctx); err != nil {
		return nil, err
	}
	sessionID := parseUUID(req.GetSessionId())
	if sessionID == uuid.Nil {
		return nil, status.Error(codes.InvalidArgument, "session_id required")
	}
	sess, err := s.service.Session(ctx, sessionID)
	if err != nil {
		return nil, s.translateError(err)
	}
	return toProtoSession(sess), nil
}

// TransitionSession applies a state transition.
func (s *Server) TransitionSession(ctx context.Context, req *realtimev1.TransitionSessionRequest) (*realtimev1.SessionRecord, error) {
	if _, err := s.claims(ctx); err != nil {
		return nil, err
	}
	sessionID := parseUUID(req.GetSessionId())
	if sessionID == uuid.Nil {
		return nil, status.Error(codes.InvalidArgument, "session_id required")
	}
	transitionReq := sessions.TransitionRequest{NextState: req.GetNextState(), MetadataPatch: mapFromStrings(req.GetMetadataPatch())}
	sess, err := s.service.TransitionSession(ctx, sessionID, transitionReq)
	if err != nil {
		return nil, s.translateError(err)
	}
	return toProtoSession(sess), nil
}

// ListParticipants lists participants for a session.
func (s *Server) ListParticipants(ctx context.Context, req *realtimev1.ParticipantsRequest) (*realtimev1.ParticipantsResponse, error) {
	if _, err := s.claims(ctx); err != nil {
		return nil, err
	}
	sessionID := parseUUID(req.GetSessionId())
	if sessionID == uuid.Nil {
		return nil, status.Error(codes.InvalidArgument, "session_id required")
	}
	participants, err := s.service.Participants(ctx, sessionID)
	if err != nil {
		return nil, s.translateError(err)
	}
	resp := &realtimev1.ParticipantsResponse{Participants: make([]*realtimev1.ParticipantRecord, 0, len(participants))}
	for _, participant := range participants {
		resp.Participants = append(resp.Participants, &realtimev1.ParticipantRecord{
			Id:        participant.ID.String(),
			SessionId: participant.SessionID.String(),
			UserId:    participant.UserID.String(),
			Role:      participant.Role,
			JoinedAt:  participant.JoinedAt.UTC().Format(time.RFC3339Nano),
			LeftAt: func() string {
				if participant.LeftAt == nil {
					return ""
				}
				return participant.LeftAt.UTC().Format(time.RFC3339Nano)
			}(),
		})
	}
	return resp, nil
}

func (s *Server) claims(ctx context.Context) (*crypto.Claims, error) {
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing credentials")
	}
	return claims, nil
}

func (s *Server) translateError(err error) error {
	switch {
	case errors.Is(err, sessions.ErrSessionNotFound):
		return status.Error(codes.NotFound, "session not found")
	case errors.Is(err, sessions.ErrInvalidMode), errors.Is(err, sessions.ErrInvalidTransition):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, sessions.ErrSessionExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, sessions.ErrSessionStateConflict):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		s.logger.Error("control rpc failed", "error", err)
		return status.Error(codes.Internal, "internal error")
	}
}

func toProtoSession(session *gormdb.Session) *realtimev1.SessionRecord {
	metadata := make(map[string]string, len(session.Metadata))
	for k, v := range session.Metadata {
		metadata[k] = stringify(v)
	}
	record := &realtimev1.SessionRecord{
		Id:            session.ID.String(),
		TenantId:      session.TenantID.String(),
		UserId:        session.UserID.String(),
		State:         session.State,
		Mode:          session.Mode,
		RequestKey:    session.RequestKey,
		Metadata:      metadata,
		StartedAt:     session.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:     session.UpdatedAt.UTC().Format(time.RFC3339Nano),
		LastHeartbeat: session.LastHeartbeat.UTC().Format(time.RFC3339Nano),
	}
	if session.EndedAt != nil {
		record.EndedAt = session.EndedAt.UTC().Format(time.RFC3339Nano)
	}
	return record
}

func mapFromStrings(src map[string]string) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func stringify(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprintf("%v", v)
	}
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
