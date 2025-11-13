package openai

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

const defaultEndpoint = "https://api.openai.com/v1/chat/completions"

func init() {
	llm.Register("openai", New)
}

// New constructs an OpenAI-compatible client.
func New(cfg llm.ProviderConfig, logger *slog.Logger) (llm.Client, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("openai api key required")
	}
	endpoint := httputil.NormalizeEndpoint(cfg.Base, defaultEndpoint)
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}
	if logger == nil {
		logger = slog.Default()
	}
	org := strings.TrimSpace(cfg.Extra["organization"])
	project := strings.TrimSpace(cfg.Extra["project"])
	temperature := parseFloat(cfg.Extra["temperature"], 0)
	return &client{
		endpoint:    endpoint,
		apiKey:      apiKey,
		model:       model,
		org:         org,
		project:     project,
		temperature: temperature,
		httpClient:  httputil.NewHTTPClient(cfg.TimeoutSeconds),
		logger:      logger.With("llm", "openai"),
	}, nil
}

type client struct {
	endpoint    string
	apiKey      string
	model       string
	org         string
	project     string
	temperature float64
	httpClient  *http.Client
	logger      *slog.Logger
}

func (c *client) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return llm.Response{}, errors.New("prompt required")
	}
	body := chatRequest{
		Model:       c.model,
		Messages:    []chatMessage{{Role: "user", Content: prompt}},
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
	if c.org != "" {
		httpReq.Header.Set("OpenAI-Organization", c.org)
	}
	if c.project != "" {
		httpReq.Header.Set("OpenAI-Project", c.project)
	}
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
	tokens := decoded.TokensUsed()
	if text == "" {
		return llm.Response{}, errors.New("openai: empty response")
	}
	return llm.Response{Text: text, TokensUsed: tokens}, nil
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
		Message chatMessageBody `json:"message"`
		Delta   chatDelta       `json:"delta"`
	} `json:"choices"`
	Usage usageStats `json:"usage"`
}

type chatMessageBody struct {
	Content json.RawMessage `json:"content"`
}

type chatDelta struct {
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
		if text := httputil.DecodeContent(choice.Delta.Content); text != "" {
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
