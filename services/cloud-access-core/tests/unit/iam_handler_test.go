package unit

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/google/uuid"
	httpapi "github.com/hayhandsome/msbd/services/cloud-access-core/internal/http"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/iam"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAuthHandlerLoginRefreshEndpoints(t *testing.T) {
	passwordHash, err := argon2id.CreateHash("ChangeMe!2024", argon2id.DefaultParams)
	require.NoError(t, err)

	repo := &stubUserRepo{
		user: &gormdb.User{
			BaseModel:    gormdb.BaseModel{ID: uuid.New()},
			TenantID:     uuid.New(),
			Email:        "admin@example.com",
			PasswordHash: []byte(passwordHash),
			Roles:        pq.StringArray{"admin"},
		},
	}

	signer, err := crypto.NewSigner(crypto.Options{Issuer: "unit-tests"})
	require.NoError(t, err)
	service := iam.NewService(
		repo,
		iam.NewInMemorySessionStore(),
		signer,
		noopAuditor{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		iam.ServiceConfig{
			WebAudience:     "web",
			DeviceAudience:  "device",
			AccessTokenTTL:  time.Minute,
			DeviceTokenTTL:  time.Minute,
			RefreshTokenTTL: time.Hour,
		},
	)
	handler := httpapi.NewAuthHandler(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	loginBody, _ := json.Marshal(map[string]any{"email": "admin@example.com", "password": "ChangeMe!2024"})
	resp, err := http.Post(server.URL+"/auth/login", "application/json", bytes.NewReader(loginBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var tokens map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&tokens))
	refreshToken := tokens["refresh_token"].(string)
	require.NotEmpty(t, refreshToken)

	refreshBody, _ := json.Marshal(map[string]any{"refresh_token": refreshToken})
	refreshResp, err := http.Post(server.URL+"/auth/refresh", "application/json", bytes.NewReader(refreshBody))
	require.NoError(t, err)
	defer refreshResp.Body.Close()
	require.Equal(t, http.StatusOK, refreshResp.StatusCode)

	badPassword, _ := json.Marshal(map[string]any{"email": "admin@example.com", "password": "wrong"})
	badResp, err := http.Post(server.URL+"/auth/login", "application/json", bytes.NewReader(badPassword))
	require.NoError(t, err)
	defer badResp.Body.Close()
	require.Equal(t, http.StatusUnauthorized, badResp.StatusCode)
}
