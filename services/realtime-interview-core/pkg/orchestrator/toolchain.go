package orchestrator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm"
)

// promptToolchain renders templates and invokes the configured LLM client.
type promptToolchain struct {
	client    llm.Client
	templates map[OutputKind]*template.Template
	maxTokens int
	logger    *slog.Logger
}

// NewPromptToolchain builds the default toolchain using template-driven prompts.
func NewPromptToolchain(cfg bootstrap.OrchestratorConfig, client llm.Client, logger *slog.Logger) (Toolchain, error) {
	if client == nil {
		return nil, errors.New("llm client required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	tmplMap, err := loadTemplates(cfg)
	if err != nil {
		return nil, err
	}
	maxTokens := cfg.MaxTokensPerWindow
	if maxTokens <= 0 {
		maxTokens = 2000
	}
	return &promptToolchain{
		client:    client,
		templates: tmplMap,
		maxTokens: maxTokens,
		logger:    logger.With("component", "prompt_toolchain"),
	}, nil
}

// Generate renders prompts for the transcript and calls the backing LLM.
func (t *promptToolchain) Generate(ctx context.Context, segment SegmentPayload) (ToolchainResult, error) {
	data := promptContext{
		SessionID:  segment.SessionID.String(),
		TenantID:   segment.TenantID.String(),
		Sequence:   segment.Sequence,
		Transcript: strings.TrimSpace(segment.Text),
		Provider:   segment.Provider,
		Confidence: segment.Confidence,
		Metadata:   segment.Metadata,
		Timestamp:  segment.CreatedAt,
	}
	if data.Timestamp.IsZero() {
		data.Timestamp = time.Now().UTC()
	}
	outputs := make([]ToolchainOutput, 0, 3)
	order := []OutputKind{OutputKindShortReply, OutputKindDetailedAnalysis, OutputKindCoachHint}
	totalTokens := 0
	for _, kind := range order {
		tmpl := t.templates[kind]
		if tmpl == nil {
			continue
		}
		prompt, err := executeTemplate(tmpl, data)
		if err != nil {
			return ToolchainResult{}, fmt.Errorf("render %s prompt: %w", kind, err)
		}
		resp, err := t.client.Complete(ctx, llm.Request{Prompt: prompt, MaxTokens: t.maxTokens})
		if err != nil {
			return ToolchainResult{}, fmt.Errorf("llm %s call failed: %w", kind, err)
		}
		totalTokens += resp.TokensUsed
		outputs = append(outputs, ToolchainOutput{Kind: kind, Text: strings.TrimSpace(resp.Text), Tokens: resp.TokensUsed})
	}
	return ToolchainResult{Outputs: outputs, TokensUsed: totalTokens}, nil
}

func loadTemplates(cfg bootstrap.OrchestratorConfig) (map[OutputKind]*template.Template, error) {
	templates := map[OutputKind]*template.Template{}
	short, err := compileTemplate("short_reply", cfg.ShortReplyTemplate, defaultShortTemplate)
	if err != nil {
		return nil, err
	}
	detailed, err := compileTemplate("detailed_analysis", cfg.DetailedTemplate, defaultDetailedTemplate)
	if err != nil {
		return nil, err
	}
	hint, err := compileTemplate("coach_hint", cfg.CoachHintTemplate, defaultHintTemplate)
	if err != nil {
		return nil, err
	}
	templates[OutputKindShortReply] = short
	templates[OutputKindDetailedAnalysis] = detailed
	templates[OutputKindCoachHint] = hint
	return templates, nil
}

func compileTemplate(name, source, fallback string) (*template.Template, error) {
	content := strings.TrimSpace(source)
	if file := detectFile(content); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", name, err)
		}
		content = string(data)
	} else if isLikelyPath(content) {
		content = ""
	}
	if content == "" {
		content = fallback
	}
	tmpl, err := template.New(name).Parse(content)
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	return tmpl, nil
}

func detectFile(value string) string {
	if value == "" {
		return ""
	}
	if strings.ContainsRune(value, '\n') {
		return ""
	}
	if _, err := os.Stat(filepath.Clean(value)); err == nil {
		return filepath.Clean(value)
	}
	return ""
}

func isLikelyPath(value string) bool {
	if value == "" {
		return false
	}
	return strings.ContainsRune(value, os.PathSeparator) || strings.Contains(value, "./") || strings.Contains(value, "..")
}

func executeTemplate(tmpl *template.Template, data any) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type promptContext struct {
	SessionID  string
	TenantID   string
	Sequence   int64
	Transcript string
	Provider   string
	Confidence float32
	Metadata   map[string]any
	Timestamp  time.Time
}

const (
	defaultShortTemplate    = "Provide a concise reply (<40 words) reacting to the latest transcript: {{.Transcript}}"
	defaultDetailedTemplate = "Analyze the interview transcript (Session {{.SessionID}}) and provide detailed coaching insights. Transcript: {{.Transcript}}"
	defaultHintTemplate     = "Craft an actionable coaching hint for tenant {{.TenantID}} based on transcript snippet: {{.Transcript}}"
)
