package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm"
)

type requestCapture struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	MaxTokens   int     `json:"max_tokens"`
	Temperature float64 `json:"temperature"`
}

func TestOpenAIComplete(t *testing.T) {
	t.Parallel()
	capture := &requestCapture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(capture))
		resp := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": "hello world",
					},
				},
			},
			"usage": map[string]any{
				"completion_tokens": 42,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(llm.ProviderConfig{
		APIKey: "secret",
		Base:   server.URL,
		Model:  "gpt-test",
	}, nil)
	require.NoError(t, err)

	resp, err := client.Complete(context.Background(), llm.Request{Prompt: "Say hi", MaxTokens: 64})
	require.NoError(t, err)
	require.Equal(t, "hello world", resp.Text)
	require.Equal(t, 42, resp.TokensUsed)
	require.Equal(t, "gpt-test", capture.Model)
	require.Equal(t, 1, len(capture.Messages))
	require.Equal(t, "user", capture.Messages[0].Role)
	require.Equal(t, "Say hi", capture.Messages[0].Content)
}
