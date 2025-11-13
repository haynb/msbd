package usage

import (
	"time"

	"github.com/google/uuid"
)

// SpeechSample represents a single observed speech provider segment.
type SpeechSample struct {
	SessionID       uuid.UUID
	TenantID        uuid.UUID
	Provider        string
	Duration        time.Duration
	ProviderLatency time.Duration
	Timestamp       time.Time
	Errored         bool
}

// LLMStat captures tokens associated with an orchestrator response.
type LLMStat struct {
	SessionID uuid.UUID
	TenantID  uuid.UUID
	Provider  string
	Tokens    int
	Timestamp time.Time
	Degraded  bool
}

// Record represents an aggregated usage window persisted to Postgres and emitted to Redis.
type Record struct {
	SessionID     uuid.UUID
	TenantID      uuid.UUID
	SpeechSeconds int
	LLMTokens     int
	ErrorCount    int
	WindowStart   time.Time
	WindowEnd     time.Time
}

// Recorder exposes the subset of methods other packages need for instrumentation.
type Recorder interface {
	RecordSpeech(sample SpeechSample)
	RecordLLM(stat LLMStat)
}
