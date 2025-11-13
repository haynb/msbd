package llm

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"sync"
)

// Request defines the data needed to execute an LLM completion.
type Request struct {
	Prompt      string
	MaxTokens   int
	Temperature float32
}

// Response represents the normalized LLM output.
type Response struct {
	Text       string
	TokensUsed int
}

// Client abstracts concrete LLM providers (OpenAI, DeepSeek, etc.).
type Client interface {
	Complete(ctx context.Context, req Request) (Response, error)
}

// ProviderConfig carries provider-specific knobs.
type ProviderConfig struct {
	Name           string
	APIKey         string
	Base           string
	Model          string
	TimeoutSeconds int
	Extra          map[string]string
}

// Factory constructs LLM clients on demand.
type Factory func(cfg ProviderConfig, logger *slog.Logger) (Client, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register associates a provider key with a factory.
func Register(name string, factory Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	key := strings.TrimSpace(strings.ToLower(name))
	if key == "" || factory == nil {
		return
	}
	registry[key] = factory
}

// NewClient constructs a client for the given provider. Falls back to the echo client.
func NewClient(provider string, cfg ProviderConfig, logger *slog.Logger) (Client, error) {
	if logger == nil {
		logger = slog.Default()
	}
	key := strings.TrimSpace(strings.ToLower(provider))
	if key == "" {
		key = "echo"
	}
	registryMu.RLock()
	factory, ok := registry[key]
	registryMu.RUnlock()
	if !ok {
		logger.Warn("llm provider not registered, using echo fallback", "provider", provider)
		return newEchoClient(cfg, logger)
	}
	client, err := factory(cfg, logger)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func init() {
	Register("echo", newEchoClient)
}

// echoClient simply converts prompts into responses for local testing.
type echoClient struct {
	logger *slog.Logger
}

func newEchoClient(cfg ProviderConfig, logger *slog.Logger) (Client, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return &echoClient{logger: logger.With("llm", "echo")}, nil
}

func (c *echoClient) Complete(ctx context.Context, req Request) (Response, error) {
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return Response{}, errors.New("prompt required")
	}
	tokens := estimateTokens(prompt)
	if req.MaxTokens > 0 && tokens > req.MaxTokens {
		tokens = req.MaxTokens
	}
	return Response{Text: prompt, TokensUsed: tokens}, nil
}

func estimateTokens(text string) int {
	words := len(strings.Fields(text))
	if words == 0 {
		return 1
	}
	est := int(math.Ceil(float64(words) * 1.5))
	if est <= 0 {
		return 1
	}
	return est
}
