package iam

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/audit"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestServiceLoginRefreshLogout(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	passwordHash, err := argon2id.CreateHash("secret", argon2id.DefaultParams)
	require.NoError(t, err)
	seedTestUser(t, repo, passwordHash)

	signer, err := crypto.NewSigner(crypto.Options{Issuer: "test"})
	require.NoError(t, err)
	service := NewService(
		repo,
		NewInMemorySessionStore(),
		signer,
		serviceTestAuditor{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceConfig{
			AccessTokenTTL:  time.Minute,
			DeviceTokenTTL:  time.Minute,
			RefreshTokenTTL: time.Hour,
			WebAudience:     "web",
			DeviceAudience:  "device",
		},
	)

	loginResult, err := service.Login(ctx, LoginRequest{Email: "admin@example.com", Password: "secret"})
	require.NoError(t, err)
	require.NotEmpty(t, loginResult.AccessToken)
	require.NotEmpty(t, loginResult.RefreshToken)

	refreshResult, err := service.Refresh(ctx, loginResult.RefreshToken)
	require.NoError(t, err)
	require.NotEqual(t, loginResult.AccessToken, refreshResult.AccessToken)

	require.NoError(t, service.Logout(ctx, refreshResult.RefreshToken))
	_, err = service.Refresh(ctx, refreshResult.RefreshToken)
	require.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestServiceLoginInvalidPassword(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	passwordHash, err := argon2id.CreateHash("secret", argon2id.DefaultParams)
	require.NoError(t, err)
	seedTestUser(t, repo, passwordHash)

	signer, err := crypto.NewSigner(crypto.Options{Issuer: "test"})
	require.NoError(t, err)
	service := NewService(
		repo,
		NewInMemorySessionStore(),
		signer,
		serviceTestAuditor{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		ServiceConfig{WebAudience: "web"},
	)
	_, err = service.Login(ctx, LoginRequest{Email: "admin@example.com", Password: "wrong"})
	require.ErrorIs(t, err, ErrInvalidCredentials)
}

func seedTestUser(t *testing.T, repo *memoryRepo, passwordHash string) {
	t.Helper()
	tenantID := uuid.New()
	userID := uuid.New()
	repo.addUser(&gormdb.User{
		BaseModel:    gormdb.BaseModel{ID: userID},
		TenantID:     tenantID,
		Email:        "admin@example.com",
		PasswordHash: []byte(passwordHash),
		Roles:        pq.StringArray{"admin"},
	})
}

type serviceTestAuditor struct{}

func (serviceTestAuditor) Record(context.Context, audit.Entry) {}

type memoryRepo struct {
	mu           sync.RWMutex
	usersByEmail map[string]*gormdb.User
	usersByID    map[uuid.UUID]*gormdb.User
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		usersByEmail: make(map[string]*gormdb.User),
		usersByID:    make(map[uuid.UUID]*gormdb.User),
	}
}

func (m *memoryRepo) addUser(user *gormdb.User) {
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := *user
	m.usersByEmail[strings.ToLower(user.Email)] = &clone
	m.usersByID[user.ID] = &clone
}

func (m *memoryRepo) UserByEmail(_ context.Context, email string) (*gormdb.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if user, ok := m.usersByEmail[strings.ToLower(email)]; ok {
		copy := *user
		return &copy, nil
	}
	return nil, gormdb.ErrNotFound
}

func (m *memoryRepo) UserByID(_ context.Context, id uuid.UUID) (*gormdb.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if user, ok := m.usersByID[id]; ok {
		copy := *user
		return &copy, nil
	}
	return nil, gormdb.ErrNotFound
}
