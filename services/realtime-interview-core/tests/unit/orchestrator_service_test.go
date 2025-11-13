package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"log/slog"

	cloudEvents "github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/orchestrator"
)

func TestOrchestratorProcessPublishesOutputs(t *testing.T) {
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		client.Close()
		mini.Close()
	})

	cfg := bootstrap.OrchestratorConfig{
		Provider:           "echo",
		ShortReplyTemplate: "Short: {{.Transcript}}",
		DetailedTemplate:   "Details: {{.Transcript}}",
		CoachHintTemplate:  "Hint: {{.Transcript}}",
		MaxTokensPerWindow: 500,
	}

	llmStub := &stubLLM{responses: []llm.Response{
		{Text: "short", TokensUsed: 50},
		{Text: "detail", TokensUsed: 75},
		{Text: "hint", TokensUsed: 25},
	}}

	toolchain, err := orchestrator.NewPromptToolchain(cfg, llmStub, slog.Default())
	require.NoError(t, err)

	publisher := &capturePublisher{}
	svc, err := orchestrator.NewService(cfg, "rtc.outputs", toolchain, publisher, client, nil, slog.Default())
	require.NoError(t, err)

	segment := orchestrator.SegmentPayload{
		SessionID:  uuid.New(),
		TenantID:   uuid.New(),
		UserID:     uuid.New(),
		Sequence:   42,
		Text:       "Candidate discussed system design",
		Provider:   "aliyun",
		Confidence: 0.91,
		Metadata:   map[string]any{"locale": "zh"},
		Final:      true,
		CreatedAt:  time.Now().UTC(),
	}

	err = svc.ProcessSegment(context.Background(), segment)
	require.NoError(t, err)
	require.Len(t, publisher.events, 3)

	kinds := []string{
		publisher.events[0].Payload["kind"].(string),
		publisher.events[1].Payload["kind"].(string),
		publisher.events[2].Payload["kind"].(string),
	}
	require.ElementsMatch(t, []string{"short_reply", "detailed_analysis", "coach_hint"}, kinds)
	for _, evt := range publisher.events {
		require.Equal(t, "rtc.outputs", evt.Topic)
		require.False(t, evt.Payload["degraded"].(bool))
	}
}

func TestOrchestratorBudgetFallbackSkipsToolchain(t *testing.T) {
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		client.Close()
		mini.Close()
	})

	cfg := bootstrap.OrchestratorConfig{
		Provider:           "echo",
		ShortReplyTemplate: "Short {{.Transcript}}",
		DetailedTemplate:   "Detailed {{.Transcript}}",
		CoachHintTemplate:  "Hint {{.Transcript}}",
		MaxTokensPerWindow: 5,
	}

	tc := &countingToolchain{}
	publisher := &capturePublisher{}
	svc, err := orchestrator.NewService(cfg, "rtc.outputs", tc, publisher, client, nil, slog.Default())
	require.NoError(t, err)

	tenantID := uuid.New()
	key := "realtime-interview-core:orch:budget:" + tenantID.String()
	require.NoError(t, client.Set(context.Background(), key, 5, cfg.BudgetWindow()).Err())

	segment := orchestrator.SegmentPayload{
		SessionID: uuid.New(),
		TenantID:  tenantID,
		Text:      "budget exhausted",
		Final:     true,
	}

	err = svc.ProcessSegment(context.Background(), segment)
	require.NoError(t, err)
	require.Equal(t, 0, tc.calls)
	require.Len(t, publisher.events, 3)
	for _, evt := range publisher.events {
		require.True(t, evt.Payload["degraded"].(bool))
		require.EqualValues(t, 0, evt.Payload["tokens_used"])
	}
}

type stubLLM struct {
	responses []llm.Response
	idx       int
}

func (s *stubLLM) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	if s.idx >= len(s.responses) {
		return llm.Response{}, errors.New("no responses left")
	}
	resp := s.responses[s.idx]
	s.idx++
	return resp, nil
}

type countingToolchain struct {
	calls int
}

func (c *countingToolchain) Generate(ctx context.Context, segment orchestrator.SegmentPayload) (orchestrator.ToolchainResult, error) {
	c.calls++
	return orchestrator.ToolchainResult{}, nil
}

type capturePublisher struct {
	events []cloudEvents.Event
}

func (c *capturePublisher) Publish(ctx context.Context, event cloudEvents.Event) error {
	c.events = append(c.events, event)
	return nil
}
