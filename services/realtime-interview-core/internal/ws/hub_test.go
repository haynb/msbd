package ws

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestHubDeliversEvents(t *testing.T) {
	hub := NewHub(nil, HubConfig{AllowAnonymous: true}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, hub.Start(ctx))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.ServeHTTP(w, r)
	}))
	defer server.Close()
	tenantID := uuid.New()
	sessionID := uuid.New()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "?tenant_id=" + tenantID.String() + "&session_id=" + sessionID.String()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = conn.ReadMessage() // ready event
	require.NoError(t, err)
	hub.Publish(hubEvent{Kind: "session.event", TenantID: tenantID, SessionID: sessionID, Payload: map[string]any{"hello": "world"}})
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(message, &payload))
	require.Equal(t, "session.event", payload["type"])
	require.Equal(t, "world", payload["payload"].(map[string]any)["hello"])
	require.NoError(t, hub.Shutdown(context.Background()))
}
