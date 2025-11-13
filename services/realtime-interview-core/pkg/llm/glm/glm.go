package glm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm/internal/httputil"
)

const (
	defaultEndpoint      = "https://open.bigmodel.cn/api/paas/v4/chat/completions"
	defaultAnthropicBase = "https://open.bigmodel.cn/api/anthropic"
)

type clientMode int

const (
	modeOpenAI clientMode = iota
	modeAnthropic
)

func init() {
	llm.Register("glm", New)
}

// New creates a Zhipu GLM client (OpenAI- or Anthropic-compatible).
func New(cfg llm.ProviderConfig, logger *slog.Logger) (llm.Client, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("glm api key required")
	}
	endpoint := httputil.NormalizeEndpoint(cfg.Base, defaultEndpoint)
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "glm-4.5"
	}
	if logger == nil {
		logger = slog.Default()
	}
	mode := detectMode(endpoint)
	endpoint = adjustEndpointForMode(endpoint, mode)
	version := strings.TrimSpace(cfg.Extra["anthropic_version"])
	if version == "" {
		version = "2023-06-01"
	}
	return &client{
		endpoint:         endpoint,
		apiKey:           apiKey,
		model:            model,
		mode:             mode,
		httpClient:       httputil.NewHTTPClient(cfg.TimeoutSeconds),
		logger:           logger.With("llm", "glm"),
		temperature:      parseFloat(cfg.Extra["temperature"], 0),
		systemPrompt:     strings.TrimSpace(cfg.Extra["system"]),
		anthropicVersion: version,
	}, nil
}

type client struct {
	endpoint         string
	apiKey           string
	model            string
	mode             clientMode
	httpClient       *http.Client
	logger           *slog.Logger
	temperature      float64
	systemPrompt     string
	anthropicVersion string
}

func (c *client) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	if c.mode == modeAnthropic {
		return c.completeAnthropic(ctx, req)
	}
	return c.completeOpenAI(ctx, req)
}

func (c *client) completeOpenAI(ctx context.Context, req llm.Request) (llm.Response, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return llm.Response{}, errors.New("prompt required")
	}
	msgs := make([]chatMessage, 0, 2)
	if c.systemPrompt != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: c.systemPrompt})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: prompt})
	body := chatRequest{
		Model:       c.model,
		Messages:    msgs,
		Temperature: chooseTemperature(req.Temperature, c.temperature),
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return llm.Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return llm.Response{}, err
	}
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return llm.Response{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return llm.Response{}, httputil.ReadBodyError(resp)
	}
	var decoded chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return llm.Response{}, err
	}
	text := decoded.Text()
	if text == "" {
		return llm.Response{}, errors.New("glm: empty response")
	}
	return llm.Response{Text: text, TokensUsed: decoded.TokensUsed()}, nil
}

func (c *client) completeAnthropic(ctx context.Context, req llm.Request) (llm.Response, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return llm.Response{}, errors.New("prompt required")
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 512
	}
	body := anthropicRequest{
		Model:       c.model,
		MaxTokens:   maxTokens,
		System:      c.systemPrompt,
		Temperature: chooseTemperature(req.Temperature, c.temperature),
		Messages: []anthropicMessage{
			{
				Role:    "user",
				Content: []anthropicContent{{Type: "text", Text: prompt}},
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return llm.Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return llm.Response{}, err
	}
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", c.anthropicVersion)
	httpReq.Header.Set("content-type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return llm.Response{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return llm.Response{}, httputil.ReadBodyError(resp)
	}
	var decoded anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return llm.Response{}, err
	}
	if decoded.Error != nil {
		return llm.Response{}, errors.New(decoded.Error.Message)
	}
	text := decoded.Text()
	if text == "" {
		return llm.Response{}, errors.New("glm: empty response")
	}
	return llm.Response{Text: text, TokensUsed: decoded.TokensUsed()}, nil
}

func detectMode(endpoint string) clientMode {
	if strings.Contains(strings.ToLower(endpoint), "anthropic") {
		return modeAnthropic
	}
	return modeOpenAI
}

func adjustEndpointForMode(endpoint string, mode clientMode) string {
	switch mode {
	case modeAnthropic:
		base := endpoint
		if base == "" {
			base = defaultAnthropicBase
		}
		base = strings.TrimRight(base, "/")
		if strings.HasSuffix(base, "/v1/messages") {
			return base
		}
		return base + "/v1/messages"
	default:
		if endpoint == "" {
			return defaultEndpoint
		}
		return endpoint
	}
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message messageBody `json:"message"`
	} `json:"choices"`
	Usage usageStats `json:"usage"`
}

type messageBody struct {
	Content json.RawMessage `json:"content"`
}

type usageStats struct {
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (r chatResponse) Text() string {
	for _, choice := range r.Choices {
		if text := httputil.DecodeContent(choice.Message.Content); text != "" {
			return text
		}
	}
	return ""
}

func (r chatResponse) TokensUsed() int {
	if r.Usage.CompletionTokens > 0 {
		return r.Usage.CompletionTokens
	}
	if r.Usage.TotalTokens > 0 {
		return r.Usage.TotalTokens
	}
	return 0
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicResponse struct {
	Content []anthropicContent `json:"content"`
	Usage   struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (r anthropicResponse) Text() string {
	for _, c := range r.Content {
		if strings.EqualFold(c.Type, "text") && strings.TrimSpace(c.Text) != "" {
			return strings.TrimSpace(c.Text)
		}
	}
	return ""
}

func (r anthropicResponse) TokensUsed() int {
	if r.Usage.OutputTokens > 0 {
		return r.Usage.OutputTokens
	}
	return 0
}

func parseFloat(value string, fallback float64) float64 {
	v := strings.TrimSpace(value)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func chooseTemperature(requested float32, configured float64) float64 {
	if requested > 0 {
		return float64(requested)
	}
	if configured > 0 {
		return configured
	}
	return 0
}
