package iam

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/audit"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/profiles"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"github.com/redis/go-redis/v9"
	"gorm.io/datatypes"
)

const (
	pairingStatusPending  = "pending"
	pairingStatusApproved = "approved"
	pairingStatusClaimed  = "claimed"
)

var (
	allowedPlatforms = map[string]string{
		"windows": "windows",
		"macos":   "macos",
		"linux":   "linux",
	}
	allowedHeartbeatStatuses = map[string]struct{}{
		"healthy":  {},
		"degraded": {},
		"suspect":  {},
	}
)

// DeviceIdentity represents the acting user/tenant context.
type DeviceIdentity struct {
	UserID   uuid.UUID
	TenantID uuid.UUID
	Email    string
}

// PairingRequest captures metadata from the desktop client.
type PairingRequest struct {
	Platform      string
	Fingerprint   string
	ClientVersion string
	Metadata      map[string]any
	DisplayName   string
}

// PairingCode contains the generated code/token pair returned to clients.
type PairingCode struct {
	Code      string
	Token     string
	ExpiresAt time.Time
}

// ApprovePairingRequest contains admin approval options.
type ApprovePairingRequest struct {
	Code        string
	ProfileName string
	TrustScore  int
}

// PairingApproval summarizes the approved device.
type PairingApproval struct {
	DeviceID      uuid.UUID
	Status        string
	ConfigVersion int
}

// ClaimPairingRequest is issued by the device after approval.
type ClaimPairingRequest struct {
	Code  string
	Token string
}

// PairingClaim delivers the encrypted profile bundle to the device.
type PairingClaim struct {
	DeviceID         uuid.UUID
	ConfigVersion    int
	EncryptedProfile string
}

// HeartbeatRequest captures device heartbeat payload.
type HeartbeatRequest struct {
	DeviceID uuid.UUID
	Status   string
	Metrics  map[string]any
}

// RevokeDeviceRequest revokes a device + notifies clients.
type RevokeDeviceRequest struct {
	DeviceID uuid.UUID
	Reason   string
}

// DeviceRepository exposes the storage subset required by DeviceService.
type DeviceRepository interface {
	SaveDevice(ctx context.Context, device *gormdb.Device) error
	DeviceByID(ctx context.Context, id uuid.UUID) (*gormdb.Device, error)
	ClientProfileByName(ctx context.Context, tenantID uuid.UUID, name string) (*gormdb.ClientProfile, error)
}

// PairingStore persists pairing records.
type PairingStore interface {
	Create(ctx context.Context, record *pairingRecord, ttl time.Duration) error
	Load(ctx context.Context, code string) (*pairingRecord, error)
	Save(ctx context.Context, record *pairingRecord, ttl time.Duration) error
	Delete(ctx context.Context, code string) error
}

// DeviceNotifier broadcasts revocation/invalidation messages.
type DeviceNotifier interface {
	BroadcastRevocation(ctx context.Context, tenantID uuid.UUID, deviceID uuid.UUID, reason string) error
}

// DeviceServiceConfig contains device-specific knobs.
type DeviceServiceConfig struct {
	PairingCodeTTL time.Duration
	CodeLength     int
	DefaultProfile string
	EncryptionKey  []byte
}

// DeviceService implements device pairing, heartbeat, and revocation flows.
type DeviceService struct {
	repo       DeviceRepository
	pairing    PairingStore
	notifier   DeviceNotifier
	publisher  events.HeartbeatPublisher
	auditor    audit.Recorder
	logger     *slog.Logger
	cfg        DeviceServiceConfig
	encryptor  *profiles.Encryptor
	codeLength int
}

// NewDeviceService builds a DeviceService instance.
func NewDeviceService(repo DeviceRepository, pairing PairingStore, notifier DeviceNotifier, publisher events.HeartbeatPublisher, auditor audit.Recorder, logger *slog.Logger, cfg DeviceServiceConfig) *DeviceService {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.PairingCodeTTL <= 0 {
		cfg.PairingCodeTTL = 10 * time.Minute
	}
	if cfg.CodeLength <= 0 {
		cfg.CodeLength = 8
	}
	if cfg.CodeLength > 32 {
		cfg.CodeLength = 32
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = "default"
	}
	encryptor := profiles.NewEncryptor(cfg.EncryptionKey)
	return &DeviceService{
		repo:       repo,
		pairing:    pairing,
		notifier:   notifier,
		publisher:  publisher,
		auditor:    auditor,
		logger:     logger.With("component", "device_service"),
		cfg:        cfg,
		encryptor:  encryptor,
		codeLength: cfg.CodeLength,
	}
}

// RequestPairing creates a new pairing challenge for a device.
func (s *DeviceService) RequestPairing(ctx context.Context, req PairingRequest) (*PairingCode, error) {
	fingerprint := strings.TrimSpace(req.Fingerprint)
	if fingerprint == "" {
		return nil, ErrInvalidPairingRequest
	}
	platform := sanitizePlatform(req.Platform)
	code, err := s.generatePairingCode()
	if err != nil {
		return nil, err
	}
	token, err := generatePairingToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	record := &pairingRecord{
		Code:              code,
		Token:             token,
		Status:            pairingStatusPending,
		RequestedAt:       now,
		ExpiresAt:         now.Add(s.cfg.PairingCodeTTL),
		DeviceFingerprint: fingerprint,
		Platform:          platform,
		ClientVersion:     req.ClientVersion,
		DisplayName:       req.DisplayName,
		Metadata:          req.Metadata,
	}
	if err := s.pairing.Create(ctx, record, s.cfg.PairingCodeTTL); err != nil {
		if errors.Is(err, ErrPairingCodeConflict) {
			// simple retry with a new code once
			code, err = s.generatePairingCode()
			if err != nil {
				return nil, err
			}
			record.Code = code
			if err := s.pairing.Create(ctx, record, s.cfg.PairingCodeTTL); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return &PairingCode{
		Code:      code,
		Token:     token,
		ExpiresAt: record.ExpiresAt,
	}, nil
}

// ApprovePairing binds the pairing record to a user/tenant and generates config payload.
func (s *DeviceService) ApprovePairing(ctx context.Context, identity DeviceIdentity, req ApprovePairingRequest) (*PairingApproval, error) {
	record, err := s.pairing.Load(ctx, req.Code)
	if err != nil {
		return nil, err
	}
	if record.Status != pairingStatusPending {
		return nil, ErrPairingPending
	}
	if time.Now().UTC().After(record.ExpiresAt) {
		return nil, ErrPairingExpired
	}

	profileName := strings.TrimSpace(req.ProfileName)
	if profileName == "" {
		profileName = s.cfg.DefaultProfile
	}
	profile, err := s.loadProfile(ctx, identity.TenantID, profileName)
	if err != nil {
		return nil, err
	}
	device := &gormdb.Device{
		BaseModel:     gormdb.BaseModel{ID: uuid.New()},
		UserID:        identity.UserID,
		TenantID:      identity.TenantID,
		Platform:      record.Platform,
		Fingerprint:   record.DeviceFingerprint,
		Status:        "approved",
		TrustScore:    int16(clamp(req.TrustScore, 0, 100)),
		LastSeen:      time.Now().UTC(),
		ConfigVersion: profile.Version,
		Metadata: datatypes.JSONMap{
			"client_version": record.ClientVersion,
			"display_name":   record.DisplayName,
			"approved_by":    identity.UserID.String(),
		},
	}
	if err := s.repo.SaveDevice(ctx, device); err != nil {
		return nil, fmt.Errorf("save device: %w", err)
	}

	bundle := profiles.FromModel(profile).WithContext(identity.TenantID, device.ID, device.Platform)
	encrypted, err := s.encryptor.Encrypt(record.Token, bundle)
	if err != nil {
		return nil, err
	}

	record.Status = pairingStatusApproved
	record.DeviceID = device.ID.String()
	record.TenantID = identity.TenantID.String()
	record.UserID = identity.UserID.String()
	record.EncryptedProfile = encrypted
	record.ConfigVersion = profile.Version
	if err := s.pairing.Save(ctx, record, time.Until(record.ExpiresAt)); err != nil {
		return nil, err
	}

	s.audit(ctx, audit.Entry{
		TenantID: identity.TenantID,
		ActorID:  identity.UserID,
		Action:   "device.pair.approve",
		Result:   "success",
		Metadata: map[string]any{
			"device_id":   device.ID.String(),
			"fingerprint": device.Fingerprint,
			"profile":     profileName,
		},
	})

	return &PairingApproval{
		DeviceID:      device.ID,
		Status:        device.Status,
		ConfigVersion: profile.Version,
	}, nil
}

// ClaimPairing allows the device to fetch the encrypted profile.
func (s *DeviceService) ClaimPairing(ctx context.Context, req ClaimPairingRequest) (*PairingClaim, error) {
	record, err := s.pairing.Load(ctx, req.Code)
	if err != nil {
		return nil, err
	}
	if record.Status != pairingStatusApproved {
		return nil, ErrPairingPending
	}
	if record.Token != req.Token {
		return nil, ErrPairingTokenMismatch
	}
	deviceID, err := uuid.Parse(record.DeviceID)
	if err != nil {
		return nil, ErrDeviceNotFound
	}
	if record.EncryptedProfile == "" {
		return nil, ErrPairingNotFound
	}
	defer func() {
		_ = s.pairing.Delete(ctx, record.Code)
	}()
	return &PairingClaim{
		DeviceID:         deviceID,
		ConfigVersion:    record.ConfigVersion,
		EncryptedProfile: record.EncryptedProfile,
	}, nil
}

// ReportHeartbeat publishes the heartbeat event to Redis for asynchronous processing.
func (s *DeviceService) ReportHeartbeat(ctx context.Context, identity DeviceIdentity, req HeartbeatRequest) error {
	if _, ok := allowedHeartbeatStatuses[strings.ToLower(req.Status)]; !ok {
		return ErrInvalidHeartbeatStatus
	}
	if s.publisher == nil {
		return nil
	}
	event := events.HeartbeatEvent{
		DeviceID:   req.DeviceID.String(),
		TenantID:   identity.TenantID.String(),
		UserID:     identity.UserID.String(),
		Status:     strings.ToLower(req.Status),
		Metrics:    req.Metrics,
		ReportedAt: time.Now().UTC(),
	}
	return s.publisher.Publish(ctx, event)
}

// RevokeDevice revokes a device and emits notification via notifier.
func (s *DeviceService) RevokeDevice(ctx context.Context, identity DeviceIdentity, req RevokeDeviceRequest) error {
	device, err := s.repo.DeviceByID(ctx, req.DeviceID)
	if err != nil {
		if errors.Is(err, gormdb.ErrNotFound) {
			return ErrDeviceNotFound
		}
		return err
	}
	if device.TenantID != identity.TenantID {
		return ErrDeviceNotFound
	}
	if device.Metadata == nil {
		device.Metadata = datatypes.JSONMap{}
	}
	device.Metadata["revoked_reason"] = req.Reason
	device.Metadata["revoked_by"] = identity.UserID.String()
	device.Metadata["revoked_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	device.Status = "revoked"
	if err := s.repo.SaveDevice(ctx, device); err != nil {
		return fmt.Errorf("update device: %w", err)
	}
	if s.notifier != nil {
		if err := s.notifier.BroadcastRevocation(ctx, identity.TenantID, device.ID, req.Reason); err != nil {
			s.logger.Warn("failed to broadcast revocation", "error", err, "device_id", device.ID)
		}
	}
	s.audit(ctx, audit.Entry{
		TenantID: identity.TenantID,
		ActorID:  identity.UserID,
		Action:   "device.revoke",
		Result:   "success",
		Metadata: map[string]any{
			"device_id": device.ID.String(),
			"reason":    req.Reason,
		},
	})
	return nil
}

func (s *DeviceService) loadProfile(ctx context.Context, tenantID uuid.UUID, profileName string) (*gormdb.ClientProfile, error) {
	profile, err := s.repo.ClientProfileByName(ctx, tenantID, profileName)
	if err != nil {
		if errors.Is(err, gormdb.ErrNotFound) {
			return &gormdb.ClientProfile{
				BaseModel:        gormdb.BaseModel{ID: uuid.New()},
				TenantID:         tenantID,
				ProfileName:      profileName,
				SamplingRate:     48000,
				NoiseGate:        0.01,
				DetectionToggles: datatypes.JSONMap{"anti_recording": true},
				ModelPresets:     datatypes.JSONMap{"llm": "default"},
				Endpoints:        datatypes.JSONMap{"grpc": "grpc://localhost:9090"},
				Version:          1,
			}, nil
		}
		return nil, fmt.Errorf("load profile: %w", err)
	}
	return profile, nil
}

func (s *DeviceService) generatePairingCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, s.codeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf), nil
}

func generatePairingToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func sanitizePlatform(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	if val, ok := allowedPlatforms[p]; ok {
		return val
	}
	return "unknown"
}

func (s *DeviceService) audit(ctx context.Context, entry audit.Entry) {
	if s.auditor != nil {
		s.auditor.Record(ctx, entry)
	}
}

func clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

type pairingRecord struct {
	Code              string         `json:"code"`
	Token             string         `json:"token"`
	Status            string         `json:"status"`
	RequestedAt       time.Time      `json:"requested_at"`
	ExpiresAt         time.Time      `json:"expires_at"`
	DeviceFingerprint string         `json:"device_fingerprint"`
	Platform          string         `json:"platform"`
	ClientVersion     string         `json:"client_version"`
	DisplayName       string         `json:"display_name"`
	Metadata          map[string]any `json:"metadata"`
	TenantID          string         `json:"tenant_id"`
	UserID            string         `json:"user_id"`
	DeviceID          string         `json:"device_id"`
	EncryptedProfile  string         `json:"encrypted_profile"`
	ConfigVersion     int            `json:"config_version"`
}

// NewRedisPairingStore builds a Redis-backed pairing store.
func NewRedisPairingStore(client *redis.Client, prefix string) PairingStore {
	if prefix == "" {
		prefix = "cloud-access-core:pairing"
	}
	return &redisPairingStore{client: client, prefix: prefix}
}

type redisPairingStore struct {
	client *redis.Client
	prefix string
}

func (s *redisPairingStore) Create(ctx context.Context, record *pairingRecord, ttl time.Duration) error {
	key := s.key(record.Code)
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal pairing: %w", err)
	}
	ok, err := s.client.SetNX(ctx, key, data, ttl).Result()
	if err != nil {
		return fmt.Errorf("save pairing: %w", err)
	}
	if !ok {
		return ErrPairingCodeConflict
	}
	return nil
}

func (s *redisPairingStore) Load(ctx context.Context, code string) (*pairingRecord, error) {
	val, err := s.client.Get(ctx, s.key(code)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrPairingNotFound
		}
		return nil, fmt.Errorf("load pairing: %w", err)
	}
	var record pairingRecord
	if err := json.Unmarshal(val, &record); err != nil {
		return nil, fmt.Errorf("decode pairing: %w", err)
	}
	if time.Now().UTC().After(record.ExpiresAt) {
		return nil, ErrPairingExpired
	}
	return &record, nil
}

func (s *redisPairingStore) Save(ctx context.Context, record *pairingRecord, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = time.Minute
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal pairing: %w", err)
	}
	if err := s.client.Set(ctx, s.key(record.Code), data, ttl).Err(); err != nil {
		return fmt.Errorf("update pairing: %w", err)
	}
	return nil
}

func (s *redisPairingStore) Delete(ctx context.Context, code string) error {
	if err := s.client.Del(ctx, s.key(code)).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

func (s *redisPairingStore) key(code string) string {
	return fmt.Sprintf("%s:%s", s.prefix, strings.ToUpper(strings.TrimSpace(code)))
}

// NewRedisDeviceNotifier creates a revocation notifier backed by Redis Pub/Sub.
func NewRedisDeviceNotifier(client *redis.Client, channel string) DeviceNotifier {
	if channel == "" {
		channel = "cloud-access-core:device-revocations"
	}
	return &redisDeviceNotifier{client: client, channel: channel}
}

type redisDeviceNotifier struct {
	client  *redis.Client
	channel string
}

func (n *redisDeviceNotifier) BroadcastRevocation(ctx context.Context, tenantID uuid.UUID, deviceID uuid.UUID, reason string) error {
	payload := map[string]any{
		"tenant_id":  tenantID.String(),
		"device_id":  deviceID.String(),
		"reason":     reason,
		"revoked_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal revocation: %w", err)
	}
	return n.client.Publish(ctx, n.channel, data).Err()
}
