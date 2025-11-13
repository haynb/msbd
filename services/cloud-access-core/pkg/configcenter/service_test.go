package configcenter

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/audit"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestServiceUpsertAndFetch(t *testing.T) {
	repo := newMemoryRepo()
	publisher := &memoryPublisher{}
	auditor := noopAuditor{}
	service := NewService(repo, publisher, auditor, nil, ServiceConfig{DefaultProfile: "default", EncryptionKey: []byte("secret"), SigningKey: []byte("signing")})

	tenantID := uuid.New()
	profile, err := service.UpsertProfile(context.Background(), ProfileSpec{
		TenantID:     tenantID,
		ProfileName:  "desktop",
		SamplingRate: 44100,
		NoiseGate:    0.02,
		Detection:    map[string]any{"anti_recording": true},
		ModelPresets: map[string]any{"llm": "edge"},
		Endpoints:    map[string]any{"grpc": "grpc://localhost:9090"},
		ActorID:      uuid.New(),
	})
	require.NoError(t, err)
	require.Equal(t, 1, profile.Version)
	require.NotEmpty(t, profile.Signature)

	fetched, err := service.GetProfile(context.Background(), tenantID, "desktop")
	require.NoError(t, err)
	require.Equal(t, profile.Version, fetched.Version)
	require.Len(t, publisher.events, 1)
}

func TestServiceEncryptBundle(t *testing.T) {
	repo := newMemoryRepo()
	tenantID := uuid.New()
	repo.save(&gormdb.ClientProfile{
		BaseModel:        gormdb.BaseModel{ID: uuid.New()},
		TenantID:         tenantID,
		ProfileName:      "default",
		SamplingRate:     48000,
		NoiseGate:        0.01,
		DetectionToggles: datatypes.JSONMap{"anti_recording": true},
		ModelPresets:     datatypes.JSONMap{"llm": "default"},
		Endpoints:        datatypes.JSONMap{"grpc": "grpc://localhost:9090"},
		Version:          2,
	})
	service := NewService(repo, nil, noopAuditor{}, nil, ServiceConfig{DefaultProfile: "default", EncryptionKey: []byte("secret"), SigningKey: []byte("signing")})
	profile, err := service.GetProfile(context.Background(), tenantID, "default")
	require.NoError(t, err)
	encrypted, err := service.EncryptBundle(profile, EncryptRequest{
		TenantID: tenantID,
		DeviceID: uuid.New(),
		Platform: "macos",
		Token:    "session",
	})
	require.NoError(t, err)
	_, err = base64.RawStdEncoding.DecodeString(encrypted)
	require.NoError(t, err)
}

// --- test doubles ---

type memoryRepo struct {
	profiles map[string]*gormdb.ClientProfile
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{profiles: make(map[string]*gormdb.ClientProfile)}
}

func (m *memoryRepo) key(tenant uuid.UUID, name string) string {
	return tenant.String() + ":" + name
}

func (m *memoryRepo) ClientProfileByName(_ context.Context, tenantID uuid.UUID, name string) (*gormdb.ClientProfile, error) {
	if profile, ok := m.profiles[m.key(tenantID, name)]; ok {
		cp := *profile
		return &cp, nil
	}
	return nil, gormdb.ErrNotFound
}

func (m *memoryRepo) SaveClientProfile(_ context.Context, profile *gormdb.ClientProfile) error {
	cp := *profile
	m.profiles[m.key(profile.TenantID, profile.ProfileName)] = &cp
	return nil
}

func (m *memoryRepo) save(profile *gormdb.ClientProfile) {
	m.profiles[m.key(profile.TenantID, profile.ProfileName)] = profile
}

type memoryPublisher struct {
	events []events.ConfigUpdateEvent
}

func (m *memoryPublisher) PublishProfileUpdate(_ context.Context, _ uuid.UUID, evt events.ConfigUpdateEvent) error {
	m.events = append(m.events, evt)
	return nil
}

type noopAuditor struct{}

func (noopAuditor) Record(context.Context, audit.Entry) {}
