package streaming

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	cloudEvents "github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/authn"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/speechgateway"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/usage"
	realtimev1 "github.com/hayhandsome/msbd/services/realtime-interview-core/proto/realtime/v1"
)

// Server exposes the streaming gRPC surface.
type Server struct {
	realtimev1.UnimplementedStreamingServiceServer
	gateway     *speechgateway.Service
	transcripts cloudEvents.Publisher
	usage       usage.Recorder
	logger      *slog.Logger
}

// NewServer builds a streaming gRPC server.
func NewServer(gateway *speechgateway.Service, transcripts cloudEvents.Publisher, usageRecorder usage.Recorder, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{gateway: gateway, transcripts: transcripts, usage: usageRecorder, logger: logger.With("component", "streaming.grpc")}
}

// StreamAudio implements the bidirectional speech stream.
func (s *Server) StreamAudio(stream realtimev1.StreamingService_StreamAudioServer) error {
	if s.gateway == nil {
		return status.Error(codes.Unavailable, "speech gateway disabled")
	}
	ctx := stream.Context()
	claims, ok := authn.ClaimsFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing credentials")
	}
	first, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "handshake failed: %v", err)
	}
	init := first.GetInit()
	if init == nil {
		return status.Error(codes.InvalidArgument, "first message must be init")
	}
	startReq := s.buildStartRequest(init, claims)
	session, err := s.gateway.OpenStream(ctx, startReq)
	if err != nil {
		return s.toStatus(err)
	}
	if err := stream.Send(&realtimev1.StreamAudioResponse{
		Payload: &realtimev1.StreamAudioResponse_Ack{
			Ack: &realtimev1.StreamAck{
				SessionId:  session.Ack().SessionID.String(),
				Provider:   session.Ack().Provider,
				SampleRate: int32(session.Ack().SampleRate),
				Format:     session.Ack().Format,
			},
		},
	}); err != nil {
		return err
	}
	sendErr := make(chan error, 1)
	go func() {
		sendErr <- s.forwardEvents(ctx, stream, session)
	}()
	if err := s.handleChunks(ctx, stream, session); err != nil {
		_ = session.Close(context.Background())
		<-sendErr
		return err
	}
	if err := session.Close(ctx); err != nil {
		s.logger.Warn("session close failed", "error", err, "session", session.ID())
	}
	return <-sendErr
}

func (s *Server) forwardEvents(ctx context.Context, stream realtimev1.StreamingService_StreamAudioServer, session *speechgateway.Session) error {
	if session == nil {
		return nil
	}
	for evt := range session.Events() {
		s.trackUsage(session, evt)
		resp := s.eventToResponse(evt)
		if err := stream.Send(resp); err != nil {
			return err
		}
		if evt.Final || evt.Type == speechgateway.EventTypeFinal {
			if err := s.publishTranscript(ctx, session, evt); err != nil {
				s.logger.Warn("publish transcript event failed", "error", err, "session", session.ID())
			}
		}
	}
	return nil
}

func (s *Server) publishTranscript(ctx context.Context, session *speechgateway.Session, evt speechgateway.ProviderEvent) error {
	if s.transcripts == nil || session == nil {
		return nil
	}
	timestamp := evt.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	payload := map[string]any{
		"action":     "transcript.final",
		"session_id": session.ID().String(),
		"tenant_id":  session.TenantID().String(),
		"user_id":    session.UserID().String(),
		"sequence":   evt.Sequence,
		"text":       evt.Text,
		"confidence": evt.Confidence,
		"provider":   evt.Provider,
		"metadata":   evt.Metadata,
		"created_at": timestamp.Format(time.RFC3339Nano),
	}
	event := cloudEvents.Event{Topic: "transcript.final", Payload: payload, Timestamp: timestamp}
	return s.transcripts.Publish(ctx, event)
}

func (s *Server) trackUsage(session *speechgateway.Session, evt speechgateway.ProviderEvent) {
	if s.usage == nil || session == nil {
		return
	}
	sample := usage.SpeechSample{
		SessionID: session.ID(),
		TenantID:  session.TenantID(),
		Provider:  evt.Provider,
		Timestamp: evt.Timestamp,
	}
	if sample.Timestamp.IsZero() {
		sample.Timestamp = time.Now().UTC()
	}
	sample.Duration = extractDuration(evt.Metadata)
	sample.ProviderLatency = extractLatency(evt.Metadata)
	if evt.Err != nil || evt.Type == speechgateway.EventTypeError {
		sample.Errored = true
	}
	s.usage.RecordSpeech(sample)
}

func extractDuration(metadata map[string]any) time.Duration {
	if metadata == nil {
		return 0
	}
	begin := parseMillis(metadata["begin_time"])
	end := parseMillis(metadata["end_time"])
	if begin <= 0 || end <= 0 || end < begin {
		return 0
	}
	return time.Duration(end-begin) * time.Millisecond
}

func extractLatency(metadata map[string]any) time.Duration {
	if metadata == nil {
		return 0
	}
	if latency := parseMillis(metadata["latency_ms"]); latency > 0 {
		return time.Duration(latency) * time.Millisecond
	}
	return 0
}

func parseMillis(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return parsed
		}
	default:
		return 0
	}
	return 0
}

func (s *Server) handleChunks(ctx context.Context, stream realtimev1.StreamingService_StreamAudioServer, session *speechgateway.Session) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		chunk := msg.GetChunk()
		if chunk == nil {
			continue
		}
		audio := speechgateway.AudioChunk{
			Sequence:    chunk.GetSequence(),
			Data:        chunk.GetData(),
			EndOfStream: chunk.GetIsLast(),
			ReceivedAt:  time.Now(),
		}
		if err := session.SendChunk(ctx, audio); err != nil {
			_ = stream.Send(&realtimev1.StreamAudioResponse{Payload: &realtimev1.StreamAudioResponse_Error{Error: s.errorPayload(err)}})
			return s.toStatus(err)
		}
		if chunk.GetIsLast() {
			return nil
		}
	}
}

func (s *Server) buildStartRequest(init *realtimev1.StreamInit, claims *crypto.Claims) speechgateway.StartRequest {
	var sessionID uuid.UUID
	if id, err := uuid.Parse(strings.TrimSpace(init.GetSessionId())); err == nil {
		sessionID = id
	}
	if sessionID == uuid.Nil {
		sessionID = uuid.New()
	}
	var tenantID uuid.UUID
	if id, err := uuid.Parse(strings.TrimSpace(claims.TenantID)); err == nil {
		tenantID = id
	}
	var userID uuid.UUID
	if id, err := uuid.Parse(strings.TrimSpace(claims.Subject)); err == nil {
		userID = id
	}
	metadata := make(map[string]string, len(init.GetMetadata()))
	for k, v := range init.GetMetadata() {
		metadata[k] = v
	}
	return speechgateway.StartRequest{
		SessionID:  sessionID,
		TenantID:   tenantID,
		UserID:     userID,
		Provider:   init.GetProvider(),
		SampleRate: int(init.GetSampleRate()),
		Format:     init.GetFormat(),
		Metadata:   metadata,
	}
}

func (s *Server) eventToResponse(evt speechgateway.ProviderEvent) *realtimev1.StreamAudioResponse {
	if evt.Err != nil || evt.Type == speechgateway.EventTypeError {
		message := "provider error"
		if evt.Err != nil {
			message = evt.Err.Error()
		}
		return &realtimev1.StreamAudioResponse{
			Payload: &realtimev1.StreamAudioResponse_Error{
				Error: &realtimev1.GatewayError{
					Code:    "PROVIDER_ERROR",
					Message: message,
				},
			},
		}
	}
	meta := make(map[string]string, len(evt.Metadata))
	for k, v := range evt.Metadata {
		meta[k] = fmt.Sprint(v)
	}
	return &realtimev1.StreamAudioResponse{
		Payload: &realtimev1.StreamAudioResponse_Transcript{
			Transcript: &realtimev1.TranscriptEvent{
				Sequence:   evt.Sequence,
				IsFinal:    evt.Final || evt.Type == speechgateway.EventTypeFinal,
				Text:       evt.Text,
				Confidence: float64(evt.Confidence),
				Provider:   evt.Provider,
				Metadata:   meta,
			},
		},
	}
}

func (s *Server) errorPayload(err error) *realtimev1.GatewayError {
	code := "STREAM_ERROR"
	var retryMillis int32
	if rl, ok := err.(speechgateway.RateLimitError); ok {
		code = "STREAM_RATE_LIMIT"
		retryMillis = int32(rl.RetryAfter.Milliseconds())
	}
	return &realtimev1.GatewayError{
		Code:             code,
		Message:          err.Error(),
		RetryAfterMillis: retryMillis,
	}
}

func (s *Server) toStatus(err error) error {
	switch {
	case errors.Is(err, speechgateway.ErrInvalidChunk):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, speechgateway.ErrOutOfOrder):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, speechgateway.ErrProviderUnavailable):
		return status.Error(codes.Unavailable, err.Error())
	default:
		var rl speechgateway.RateLimitError
		if errors.As(err, &rl) {
			return status.Error(codes.ResourceExhausted, rl.Error())
		}
		return status.Error(codes.Internal, err.Error())
	}
}
