package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/usage"
)

// Service coordinates transcript-to-AI orchestration.
type Service struct {
	cfg          bootstrap.OrchestratorConfig
	toolchain    Toolchain
	publisher    events.Publisher
	redis        redis.Cmdable
	outputTopic  string
	budgetPrefix string
	usage        usage.Recorder
	logger       *slog.Logger
	now          func() time.Time
}

// NewService wires orchestrator dependencies together.
func NewService(cfg bootstrap.OrchestratorConfig, outputTopic string, toolchain Toolchain, publisher events.Publisher, redisClient redis.Cmdable, usageRecorder usage.Recorder, logger *slog.Logger) (*Service, error) {
	if toolchain == nil {
		return nil, errors.New("toolchain required")
	}
	if publisher == nil {
		return nil, errors.New("publisher required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	topic := strings.TrimSpace(outputTopic)
	if topic == "" {
		topic = cfg.QueueStream
	}
	svc := &Service{
		cfg:          cfg,
		toolchain:    toolchain,
		publisher:    publisher,
		redis:        redisClient,
		outputTopic:  topic,
		budgetPrefix: "realtime-interview-core:orch:budget",
		usage:        usageRecorder,
		logger:       logger.With("component", "orchestrator"),
		now:          time.Now,
	}
	return svc, nil
}

// SetClock overrides the time source (primarily for tests).
func (s *Service) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// ProcessSegment ingests a final transcript segment and publishes AI outputs.
func (s *Service) ProcessSegment(ctx context.Context, segment SegmentPayload) error {
	if !segment.Final {
		return nil
	}
	segment.Text = strings.TrimSpace(segment.Text)
	if segment.Text == "" {
		return errors.New("segment text required")
	}
	if segment.SessionID == uuid.Nil {
		return errors.New("session id required")
	}
	if segment.CreatedAt.IsZero() {
		segment.CreatedAt = s.now().UTC()
	}

	allowed := s.hasBudget(ctx, segment.TenantID)
	degraded := false
	tokensUsed := 0
	outputs := make([]ToolchainOutput, 0, 3)
	start := s.now()
	var genErr error

	if allowed {
		result, err := s.toolchain.Generate(ctx, segment)
		if err != nil {
			s.logger.Warn("toolchain failed", "error", err, "session", segment.SessionID)
			degraded = true
			genErr = err
			outputs = s.fallbackOutputs(segment)
		} else {
			outputs = result.Outputs
			if len(outputs) == 0 {
				degraded = true
				outputs = s.fallbackOutputs(segment)
			} else {
				tokensUsed = result.TokensUsed
				if tokensUsed > 0 {
					if err := s.incrementBudget(ctx, segment.TenantID, tokensUsed); err != nil {
						s.logger.Warn("budget increment failed", "error", err, "tenant", segment.TenantID)
					}
				}
			}
		}
	} else {
		degraded = true
		outputs = s.fallbackOutputs(segment)
	}

	if len(outputs) == 0 {
		return genErr
	}

	latency := s.now().Sub(start)
	var publishErr error
	for _, out := range outputs {
		payload := map[string]any{
			"action":      "orchestrator.output",
			"session_id":  segment.SessionID.String(),
			"tenant_id":   segment.TenantID.String(),
			"user_id":     segment.UserID.String(),
			"sequence":    segment.Sequence,
			"kind":        string(out.Kind),
			"text":        out.Text,
			"provider":    segment.Provider,
			"confidence":  segment.Confidence,
			"degraded":    degraded,
			"tokens_used": out.Tokens,
			"latency_ms":  latency.Milliseconds(),
			"created_at":  segment.CreatedAt.UTC().Format(time.RFC3339Nano),
			"metadata":    segment.Metadata,
		}
		evt := events.Event{Topic: s.outputTopic, Payload: payload, Timestamp: segment.CreatedAt}
		if err := s.publisher.Publish(ctx, evt); err != nil {
			s.logger.Error("publish orchestrator output failed", "error", err, "kind", out.Kind, "session", segment.SessionID)
			publishErr = err
		}
	}
	if publishErr != nil {
		return publishErr
	}
	s.recordUsage(segment, tokensUsed, degraded)
	return genErr
}

func (s *Service) recordUsage(segment SegmentPayload, tokens int, degraded bool) {
	if s == nil || s.usage == nil || segment.SessionID == uuid.Nil {
		return
	}
	stat := usage.LLMStat{
		SessionID: segment.SessionID,
		TenantID:  segment.TenantID,
		Provider:  s.cfg.Provider,
		Tokens:    tokens,
		Timestamp: s.now().UTC(),
		Degraded:  degraded,
	}
	s.usage.RecordLLM(stat)
}

func (s *Service) hasBudget(ctx context.Context, tenant uuid.UUID) bool {
	if s.redis == nil || tenant == uuid.Nil || s.cfg.MaxTokensPerWindow <= 0 {
		return true
	}
	val, err := s.redis.Get(ctx, s.budgetKey(tenant)).Int()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			s.logger.Warn("budget lookup failed", "error", err, "tenant", tenant)
		}
		return true
	}
	return val < s.cfg.MaxTokensPerWindow
}

func (s *Service) incrementBudget(ctx context.Context, tenant uuid.UUID, tokens int) error {
	if s.redis == nil || tenant == uuid.Nil || tokens <= 0 || s.cfg.MaxTokensPerWindow <= 0 {
		return nil
	}
	key := s.budgetKey(tenant)
	if err := s.redis.IncrBy(ctx, key, int64(tokens)).Err(); err != nil {
		return err
	}
	if err := s.redis.Expire(ctx, key, s.cfg.BudgetWindow()).Err(); err != nil && !errors.Is(err, redis.Nil) {
		s.logger.Debug("budget expire failed", "error", err)
	}
	return nil
}

func (s *Service) budgetKey(tenant uuid.UUID) string {
	prefix := strings.TrimSpace(s.budgetPrefix)
	if prefix == "" {
		prefix = "orchestrator:budget"
	}
	return fmt.Sprintf("%s:%s", prefix, tenant.String())
}

func (s *Service) fallbackOutputs(segment SegmentPayload) []ToolchainOutput {
	snippet := segment.Text
	if len(snippet) > 240 {
		snippet = snippet[:240] + "..."
	}
	base := fmt.Sprintf("AI coach unavailable for session %s; latest transcript: %s", segment.SessionID, snippet)
	hint := fmt.Sprintf("Follow up manually with the candidate. Sequence %d, tenant %s.", segment.Sequence, segment.TenantID)
	return []ToolchainOutput{
		{Kind: OutputKindShortReply, Text: base, Tokens: 0},
		{Kind: OutputKindDetailedAnalysis, Text: base, Tokens: 0},
		{Kind: OutputKindCoachHint, Text: hint, Tokens: 0},
	}
}
