package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm/internal/httputil"
)

const defaultEndpoint = "https://api.deepseek.com/chat/completions"

func init() {
	llm.Register("deepseek", New)
}

// New creates a DeepSeek API client.
func New(cfg llm.ProviderConfig, logger *slog.Logger) (llm.Client, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, errors.New("deepseek api key required")
	}
	endpoint := httputil.NormalizeEndpoint(cfg.Base, defaultEndpoint)
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "deepseek-chat"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &client{
		endpoint:   endpoint,
		apiKey:     apiKey,
		model:      model,
		httpClient: httputil.NewHTTPClient(cfg.TimeoutSeconds),
		logger:     logger.With("llm", "deepseek"),
	}, nil
}

type client struct {
	endpoint   string
	apiKey     string
	model      string
	httpClient *http.Client
	logger     *slog.Logger
}

func (c *client) Complete(ctx context.Context, req llm.Request) (llm.Response, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return llm.Response{}, errors.New("prompt required")
	}
	body := chatRequest{
		Model:    c.model,
		Messages: []chatMessage{{Role: "user", Content: prompt}},
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}
	if req.Temperature > 0 {
		body.Temperature = float64(req.Temperature)
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
	tokens := decoded.TokensUsed()
	if text == "" {
		return llm.Response{}, errors.New("deepseek: empty response")
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
		Message messageBody `json:"message"`
		Delta   messageBody `json:"delta"`
	} `json:"choices"`
	Usage usageStats `json:"usage"`
}

type messageBody struct {
	Content          json.RawMessage `json:"content"`
	ReasoningContent json.RawMessage `json:"reasoning_content"`
}

type usageStats struct {
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (r chatResponse) Text() string {
	for _, choice := range r.Choices {
		if text := combine(choice.Message); text != "" {
			return text
		}
		if text := combine(choice.Delta); text != "" {
			return text
		}
	}
	return ""
}

func combine(body messageBody) string {
	content := strings.TrimSpace(httputil.DecodeContent(body.Content))
	reasoning := strings.TrimSpace(httputil.DecodeContent(body.ReasoningContent))
	parts := make([]string, 0, 2)
	if reasoning != "" {
		parts = append(parts, reasoning)
	}
	if content != "" {
		parts = append(parts, content)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
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
