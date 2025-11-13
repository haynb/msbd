package speechgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ErrProviderUnavailable indicates the backing provider cannot accept work.
var ErrProviderUnavailable = errors.New("speech provider unavailable")

// ErrInvalidChunk indicates a malformed or unexpected audio chunk.
var ErrInvalidChunk = errors.New("invalid audio chunk")

// ErrOutOfOrder indicates a non-monotonic chunk sequence.
var ErrOutOfOrder = errors.New("chunk sequence out of order")

// Provider defines the behaviour speech adapters must implement.
type Provider interface {
	Start(ctx context.Context, params StartRequest) (StreamHandle, error)
}

// StreamHandle represents an active provider connection.
type StreamHandle interface {
	SendAudio(ctx context.Context, chunk AudioChunk) error
	Close(ctx context.Context) error
	Events() <-chan ProviderEvent
}

// StartRequest conveys handshake metadata to providers.
type StartRequest struct {
	SessionID   uuid.UUID
	TenantID    uuid.UUID
	UserID      uuid.UUID
	Provider    string
	SampleRate  int
	Format      string
	Metadata    map[string]string
	AckDeadline time.Duration
}

// AudioChunk carries PCM payloads from clients.
type AudioChunk struct {
	Sequence    int64
	Data        []byte
	EndOfStream bool
	ReceivedAt  time.Time
}

// EventType categorizes provider callbacks.
type EventType string

const (
	// EventTypePartial indicates an interim transcript.
	EventTypePartial EventType = "partial"
	// EventTypeFinal indicates a finalized transcript segment.
	EventTypeFinal EventType = "final"
	// EventTypeInfo represents auxiliary notifications (sentence begin/end).
	EventTypeInfo EventType = "info"
	// EventTypeError surfaces provider-side failures.
	EventTypeError EventType = "error"
)

// ProviderEvent normalizes adapter callbacks for upstream consumers.
type ProviderEvent struct {
	Provider   string
	Type       EventType
	Sequence   int64
	Text       string
	Confidence float32
	Final      bool
	Metadata   map[string]any
	Raw        json.RawMessage
	Err        error
	Timestamp  time.Time
}

// SessionAck summarises negotiated stream parameters.
type SessionAck struct {
	SessionID  uuid.UUID
	Provider   string
	SampleRate int
	Format     string
}

// RateLimitError indicates the caller exceeded ingest quotas.
type RateLimitError struct {
	RetryAfter time.Duration
}

// Error implements the error interface.
func (e RateLimitError) Error() string {
	if e.RetryAfter <= 0 {
		return "stream rate limited"
	}
	return fmt.Sprintf("stream rate limited, retry after %s", e.RetryAfter)
}
