package glm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm"
)

func TestGLMComplete(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer glm-key", r.Header.Get("Authorization"))
		resp := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": []any{
							map[string]any{"type": "text", "text": "glm answer"},
						},
					},
				},
			},
			"usage": map[string]any{
				"total_tokens": 12,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(llm.ProviderConfig{APIKey: "glm-key", Base: server.URL, Model: "GLM-4.6"}, nil)
	require.NoError(t, err)

	resp, err := client.Complete(context.Background(), llm.Request{Prompt: "hi"})
	require.NoError(t, err)
	require.Equal(t, "glm answer", resp.Text)
	require.Equal(t, 12, resp.TokensUsed)
}

func TestGLMCompleteAnthropic(t *testing.T) {
	t.Parallel()
	var captured anthropicRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-key", r.Header.Get("x-api-key"))
		require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		require.True(t, strings.HasSuffix(r.URL.Path, "/v1/messages"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		resp := map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "anthropic reply"},
			},
			"usage": map[string]any{"output_tokens": 33},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	base := server.URL + "/anthropic"
	client, err := New(llm.ProviderConfig{APIKey: "test-key", Base: base, Model: "GLM-4.6"}, nil)
	require.NoError(t, err)

	resp, err := client.Complete(context.Background(), llm.Request{Prompt: "say hi", MaxTokens: 64})
	require.NoError(t, err)
	require.Equal(t, "anthropic reply", resp.Text)
	require.Equal(t, 33, resp.TokensUsed)
	require.Equal(t, 64, captured.MaxTokens)
	require.Equal(t, "say hi", captured.Messages[0].Content[0].Text)
}
