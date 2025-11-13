package orchestrator

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// OutputKind categorizes orchestrator payloads.
type OutputKind string

const (
	// OutputKindShortReply is the quick nudge for interviewers.
	OutputKindShortReply OutputKind = "short_reply"
	// OutputKindDetailedAnalysis contains the longer AI breakdown.
	OutputKindDetailedAnalysis OutputKind = "detailed_analysis"
	// OutputKindCoachHint provides actionable coaching tips.
	OutputKindCoachHint OutputKind = "coach_hint"
)

// SegmentPayload represents a finalized transcript segment destined for orchestration.
type SegmentPayload struct {
	SessionID  uuid.UUID
	TenantID   uuid.UUID
	UserID     uuid.UUID
	Sequence   int64
	Text       string
	Provider   string
	Confidence float32
	Metadata   map[string]any
	Final      bool
	CreatedAt  time.Time
}

// ToolchainOutput is a generated AI payload paired with token usage.
type ToolchainOutput struct {
	Kind   OutputKind
	Text   string
	Tokens int
}

// ToolchainResult aggregates outputs from the LLM toolchain.
type ToolchainResult struct {
	Outputs    []ToolchainOutput
	TokensUsed int
}

// Toolchain turns transcript segments into orchestrated outputs.
type Toolchain interface {
	Generate(ctx context.Context, segment SegmentPayload) (ToolchainResult, error)
}
