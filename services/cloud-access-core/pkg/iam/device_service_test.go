package iam

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/audit"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"github.com/lib/pq"
	"gorm.io/datatypes"
)

func TestDeviceServicePairingFlow(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryDeviceRepo()
	userID := uuid.New()
	tenantID := uuid.New()
	repo.users[userID] = &gormdb.User{
		BaseModel:    gormdb.BaseModel{ID: userID},
		TenantID:     tenantID,
		Email:        "user@example.com",
		PasswordHash: []byte("hash"),
		Roles:        pq.StringArray{"admin"},
	}

	pairingStore := newMemoryPairingStore()
	notifier := &memoryNotifier{}
	publisher := &memoryPublisher{}
	service := NewDeviceService(
		repo,
		pairingStore,
		notifier,
		publisher,
		noopAuditor{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		DeviceServiceConfig{PairingCodeTTL: time.Minute, CodeLength: 6},
	)

	code, err := service.RequestPairing(ctx, PairingRequest{
		Platform:    "macos",
		Fingerprint: "fp-123",
	})
	if err != nil {
		t.Fatalf("request pairing: %v", err)
	}
	if code.Code == "" || code.Token == "" {
		t.Fatalf("expected code/token to be set")
	}

	identity := DeviceIdentity{UserID: userID, TenantID: tenantID, Email: "user@example.com"}
	approval, err := service.ApprovePairing(ctx, identity, ApprovePairingRequest{Code: code.Code})
	if err != nil {
		t.Fatalf("approve pairing: %v", err)
	}
	if approval.DeviceID == uuid.Nil {
		t.Fatalf("expected device id")
	}

	claim, err := service.ClaimPairing(ctx, ClaimPairingRequest{Code: code.Code, Token: code.Token})
	if err != nil {
		t.Fatalf("claim pairing: %v", err)
	}
	if claim.EncryptedProfile == "" {
		t.Fatalf("expected encrypted profile")
	}
}

func TestDeviceServiceHeartbeatPublish(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryDeviceRepo()
	pairingStore := newMemoryPairingStore()
	publisher := &memoryPublisher{}
	service := NewDeviceService(
		repo,
		pairingStore,
		&memoryNotifier{},
		publisher,
		noopAuditor{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		DeviceServiceConfig{},
	)

	deviceID := uuid.New()
	identity := DeviceIdentity{UserID: uuid.New(), TenantID: uuid.New()}
	err := service.ReportHeartbeat(ctx, identity, HeartbeatRequest{
		DeviceID: deviceID,
		Status:   "healthy",
		Metrics:  map[string]any{"latency_ms": 12},
	})
	if err != nil {
		t.Fatalf("report heartbeat: %v", err)
	}

	if len(publisher.events) != 1 || publisher.events[0].DeviceID != deviceID.String() {
		t.Fatalf("expected heartbeat event to be published")
	}
}

// --- test doubles ---

type memoryPairingStore struct {
	records map[string]*pairingRecord
}

func newMemoryPairingStore() *memoryPairingStore {
	return &memoryPairingStore{records: make(map[string]*pairingRecord)}
}

func (m *memoryPairingStore) Create(_ context.Context, record *pairingRecord, _ time.Duration) error {
	if _, ok := m.records[record.Code]; ok {
		return ErrPairingCodeConflict
	}
	cp := *record
	m.records[record.Code] = &cp
	return nil
}

func (m *memoryPairingStore) Load(_ context.Context, code string) (*pairingRecord, error) {
	if rec, ok := m.records[code]; ok {
		cp := *rec
		return &cp, nil
	}
	return nil, ErrPairingNotFound
}

func (m *memoryPairingStore) Save(_ context.Context, record *pairingRecord, _ time.Duration) error {
	cp := *record
	m.records[record.Code] = &cp
	return nil
}

func (m *memoryPairingStore) Delete(_ context.Context, code string) error {
	delete(m.records, code)
	return nil
}

type memoryDeviceRepo struct {
	users   map[uuid.UUID]*gormdb.User
	devices map[uuid.UUID]*gormdb.Device
}

func newMemoryDeviceRepo() *memoryDeviceRepo {
	return &memoryDeviceRepo{
		users:   make(map[uuid.UUID]*gormdb.User),
		devices: make(map[uuid.UUID]*gormdb.Device),
	}
}

func (m *memoryDeviceRepo) SaveDevice(_ context.Context, device *gormdb.Device) error {
	cp := *device
	m.devices[device.ID] = &cp
	return nil
}

func (m *memoryDeviceRepo) DeviceByID(_ context.Context, id uuid.UUID) (*gormdb.Device, error) {
	if device, ok := m.devices[id]; ok {
		cp := *device
		return &cp, nil
	}
	return nil, gormdb.ErrNotFound
}

func (m *memoryDeviceRepo) ClientProfileByName(_ context.Context, _ uuid.UUID, _ string) (*gormdb.ClientProfile, error) {
	return &gormdb.ClientProfile{
		ProfileName:      "default",
		SamplingRate:     48000,
		NoiseGate:        0.01,
		DetectionToggles: datatypes.JSONMap{"anti_recording": true},
		ModelPresets:     datatypes.JSONMap{"llm": "default"},
		Endpoints:        datatypes.JSONMap{"grpc": "grpc://localhost:9090"},
		Version:          1,
	}, nil
}

type memoryNotifier struct {
	last struct {
		Tenant uuid.UUID
		Device uuid.UUID
		Reason string
	}
}

func (m *memoryNotifier) BroadcastRevocation(_ context.Context, tenantID uuid.UUID, deviceID uuid.UUID, reason string) error {
	m.last.Tenant = tenantID
	m.last.Device = deviceID
	m.last.Reason = reason
	return nil
}

type memoryPublisher struct {
	events []events.HeartbeatEvent
}

func (m *memoryPublisher) Publish(_ context.Context, event events.HeartbeatEvent) error {
	m.events = append(m.events, event)
	return nil
}

type noopAuditor struct{}

func (noopAuditor) Record(context.Context, audit.Entry) {}
