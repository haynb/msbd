package deepseek

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/llm"
)

func TestDeepSeekComplete(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer ds-key", r.Header.Get("Authorization"))
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, "deepseek-reasoner", payload["model"])
		resp := map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content":           "analysis",
						"reasoning_content": "chain",
					},
				},
			},
			"usage": map[string]any{
				"completion_tokens": 21,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client, err := New(llm.ProviderConfig{APIKey: "ds-key", Base: server.URL, Model: "deepseek-reasoner"}, nil)
	require.NoError(t, err)

	resp, err := client.Complete(context.Background(), llm.Request{Prompt: "explain"})
	require.NoError(t, err)
	require.Equal(t, "chain analysis", resp.Text)
	require.Equal(t, 21, resp.TokensUsed)
}
