package unit

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	httpapi "github.com/hayhandsome/msbd/services/realtime-interview-core/internal/http"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/authn"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/speechgateway"
)

func TestProviderHandlerRuntimeEndpoints(t *testing.T) {
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	store := speechgateway.NewConfigStore(client, "test:runtime", "test:channel", slog.New(slog.NewTextHandler(io.Discard, nil)))

	require.NoError(t, store.Save(context.Background(), speechgateway.RuntimeConfig{DefaultProvider: "aliyun"}))

	handler := httpapi.NewProviderHandler(fakeGateway{}, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := chi.NewRouter()
	handler.Register(router)

	adminClaims := &crypto.Claims{Roles: []string{"admin"}}

	req := httptest.NewRequest(http.MethodGet, "/providers/runtime", nil)
	req = req.WithContext(authn.WithClaims(req.Context(), adminClaims))
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), "aliyun")

	payload := []byte(`{"default_provider":"custom"}`)
	putReq := httptest.NewRequest(http.MethodPut, "/providers/runtime", bytes.NewReader(payload))
	putReq.Header.Set("Content-Type", "application/json")
	putReq = putReq.WithContext(authn.WithClaims(putReq.Context(), adminClaims))
	putResp := httptest.NewRecorder()
	router.ServeHTTP(putResp, putReq)
	require.Equal(t, http.StatusAccepted, putResp.Code)

	cfg, err := store.Load(context.Background())
	require.NoError(t, err)
	require.Equal(t, "custom", cfg.DefaultProvider)
}

func TestProviderHandlerRuntimeRequiresAdmin(t *testing.T) {
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	store := speechgateway.NewConfigStore(client, "test:runtime", "test:channel", slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := httpapi.NewProviderHandler(fakeGateway{}, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	router := chi.NewRouter()
	handler.Register(router)

	req := httptest.NewRequest(http.MethodGet, "/providers/runtime", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusUnauthorized, resp.Code)
}

type fakeGateway struct{}

func (fakeGateway) Status() speechgateway.GatewayStatus {
	return speechgateway.GatewayStatus{DefaultProvider: "aliyun"}
}
