package unit

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/alexedwards/argon2id"
	"github.com/google/uuid"
	httpapi "github.com/hayhandsome/msbd/services/cloud-access-core/internal/http"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/configcenter"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/iam"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestConfigHandler_ProfileLifecycle(t *testing.T) {
	passwordHash, err := argon2id.CreateHash("ChangeMe!2024", argon2id.DefaultParams)
	require.NoError(t, err)

	tenantID := uuid.New()
	userID := uuid.New()
	deviceID := uuid.New()

	userRepo := &stubUserRepo{
		user: &gormdb.User{
			BaseModel:    gormdb.BaseModel{ID: userID},
			TenantID:     tenantID,
			Email:        "admin@example.com",
			PasswordHash: []byte(passwordHash),
			Roles:        pq.StringArray{"admin"},
		},
	}

	deviceStore := newMemoryDeviceStore()
	deviceStore.save(&gormdb.Device{
		BaseModel:   gormdb.BaseModel{ID: deviceID},
		UserID:      userID,
		TenantID:    tenantID,
		Platform:    "macos",
		Fingerprint: "fp-123",
		Status:      "approved",
	})

	profileRepo := newMemoryProfileRepo()

	signer, err := crypto.NewSigner(crypto.Options{Issuer: "unit-tests"})
	require.NoError(t, err)
	iamSvc := iam.NewService(
		userRepo,
		iam.NewInMemorySessionStore(),
		signer,
		noopAuditor{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		iam.ServiceConfig{WebAudience: "web", DeviceAudience: "device"},
	)
	cfgSvc := configcenter.NewService(
		profileRepo,
		nil,
		noopAuditor{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		configcenter.ServiceConfig{
			DefaultProfile: "default",
			EncryptionKey:  []byte("0123456789abcdef0123456789abcdef"),
			SigningKey:     []byte("0123456789abcdef0123456789abcdef"),
		},
	)
	handler := httpapi.NewConfigHandler(iamSvc, deviceStore, cfgSvc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	login, err := iamSvc.Login(t.Context(), iam.LoginRequest{Email: "admin@example.com", Password: "ChangeMe!2024"})
	require.NoError(t, err)

	upsertBody, _ := json.Marshal(map[string]any{
		"tenant_id":     tenantID.String(),
		"profile_name":  "desktop",
		"sampling_rate": 44100,
		"noise_gate":    0.02,
		"detection":     map[string]any{"anti_recording": true},
		"model_presets": map[string]any{"llm": "edge"},
	})
	req, err := http.NewRequest(http.MethodPut, server.URL+"/configs/profiles", bytes.NewReader(upsertBody))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+login.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	fetchURL := server.URL + "/configs/profile?device_id=" + deviceID.String() + "&profile=desktop"
	fetchReq, err := http.NewRequest(http.MethodGet, fetchURL, nil)
	require.NoError(t, err)
	fetchReq.Header.Set("Authorization", "Bearer "+login.DeviceToken)
	fetchResp, err := http.DefaultClient.Do(fetchReq)
	require.NoError(t, err)
	defer fetchResp.Body.Close()
	require.Equal(t, http.StatusOK, fetchResp.StatusCode)
	var payload map[string]any
	require.NoError(t, json.NewDecoder(fetchResp.Body).Decode(&payload))
	require.Equal(t, "desktop", payload["profile_name"])
	require.NotEmpty(t, payload["encrypted_profile"])
}

// --- test doubles ---

type memoryDeviceStore struct {
	mu      sync.RWMutex
	devices map[uuid.UUID]*gormdb.Device
}

func newMemoryDeviceStore() *memoryDeviceStore {
	return &memoryDeviceStore{devices: make(map[uuid.UUID]*gormdb.Device)}
}

func (m *memoryDeviceStore) save(device *gormdb.Device) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *device
	m.devices[device.ID] = &cp
}

func (m *memoryDeviceStore) DeviceByID(_ context.Context, id uuid.UUID) (*gormdb.Device, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if device, ok := m.devices[id]; ok {
		cp := *device
		return &cp, nil
	}
	return nil, gormdb.ErrNotFound
}

type memoryProfileRepo struct {
	mu       sync.RWMutex
	profiles map[string]*gormdb.ClientProfile
}

func newMemoryProfileRepo() *memoryProfileRepo {
	return &memoryProfileRepo{profiles: make(map[string]*gormdb.ClientProfile)}
}

func (m *memoryProfileRepo) key(tenant uuid.UUID, name string) string {
	return tenant.String() + ":" + name
}

func (m *memoryProfileRepo) ClientProfileByName(_ context.Context, tenantID uuid.UUID, name string) (*gormdb.ClientProfile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if profile, ok := m.profiles[m.key(tenantID, name)]; ok {
		cp := *profile
		return &cp, nil
	}
	return nil, gormdb.ErrNotFound
}

func (m *memoryProfileRepo) SaveClientProfile(_ context.Context, profile *gormdb.ClientProfile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *profile
	if cp.DetectionToggles == nil {
		cp.DetectionToggles = datatypes.JSONMap{}
	}
	if cp.ModelPresets == nil {
		cp.ModelPresets = datatypes.JSONMap{}
	}
	if cp.Endpoints == nil {
		cp.Endpoints = datatypes.JSONMap{}
	}
	m.profiles[m.key(profile.TenantID, profile.ProfileName)] = &cp
	return nil
}
