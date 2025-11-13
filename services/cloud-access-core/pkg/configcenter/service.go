package configcenter

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
	"gorm.io/datatypes"
)

// ServiceConfig describes runtime knobs for the config center service.
type ServiceConfig struct {
	DefaultProfile string
	EncryptionKey  []byte
	SigningKey     []byte
}

// Service manages profile CRUD, signing, and bundle encryption.
type Service struct {
	repo      Repository
	publisher events.ConfigPublisher
	encryptor *profiles.Encryptor
	auditor   audit.Recorder
	logger    *slog.Logger
	cfg       ServiceConfig
}

// NewService constructs a config center service instance.
func NewService(repo Repository, publisher events.ConfigPublisher, auditor audit.Recorder, logger *slog.Logger, cfg ServiceConfig) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.DefaultProfile == "" {
		cfg.DefaultProfile = "default"
	}
	encryptor := profiles.NewEncryptor(cfg.EncryptionKey)
	return &Service{
		repo:      repo,
		publisher: publisher,
		encryptor: encryptor,
		auditor:   auditor,
		logger:    logger.With("component", "config_service"),
		cfg:       cfg,
	}
}

// DefaultProfileName returns the configured fallback profile name.
func (s *Service) DefaultProfileName() string {
	if s == nil || s.cfg.DefaultProfile == "" {
		return "default"
	}
	return s.cfg.DefaultProfile
}

// UpsertProfile creates or updates a client profile for the tenant.
func (s *Service) UpsertProfile(ctx context.Context, spec ProfileSpec) (*gormdb.ClientProfile, error) {
	if spec.TenantID == uuid.Nil {
		return nil, ErrInvalidRequest
	}
	name := s.normalizeProfileName(spec.ProfileName)
	sampling := spec.SamplingRate
	if sampling <= 0 {
		sampling = 48000
	}
	noise := spec.NoiseGate
	if noise <= 0 {
		noise = 0.01
	}

	existing, err := s.repo.ClientProfileByName(ctx, spec.TenantID, name)
	if err != nil {
		if !errors.Is(err, gormdb.ErrNotFound) {
			return nil, fmt.Errorf("load profile: %w", err)
		}
		existing = nil
	}

	version := 1
	baseModel := gormdb.BaseModel{ID: uuid.New()}
	if existing != nil {
		version = existing.Version + 1
		baseModel = existing.BaseModel
	}

	profile := &gormdb.ClientProfile{
		BaseModel:        baseModel,
		TenantID:         spec.TenantID,
		ProfileName:      name,
		SamplingRate:     sampling,
		NoiseGate:        noise,
		DetectionToggles: toJSONMap(spec.Detection),
		ModelPresets:     toJSONMap(spec.ModelPresets),
		Endpoints:        toJSONMap(spec.Endpoints),
		Version:          version,
	}
	profile.Signature = s.signProfile(profile)

	if err := s.repo.SaveClientProfile(ctx, profile); err != nil {
		return nil, fmt.Errorf("save profile: %w", err)
	}

	s.publishUpdate(ctx, spec.TenantID, name, version)
	s.audit(ctx, audit.Entry{
		TenantID: spec.TenantID,
		ActorID:  spec.ActorID,
		Action:   "config.profile.save",
		Result:   "success",
		Metadata: map[string]any{"profile": name, "version": version},
	})

	return profile, nil
}

// GetProfile fetches the profile by name or returns a default stub if missing.
func (s *Service) GetProfile(ctx context.Context, tenantID uuid.UUID, profileName string) (*gormdb.ClientProfile, error) {
	if tenantID == uuid.Nil {
		return nil, ErrInvalidRequest
	}
	name := s.normalizeProfileName(profileName)
	profile, err := s.repo.ClientProfileByName(ctx, tenantID, name)
	if err != nil {
		if errors.Is(err, gormdb.ErrNotFound) {
			return s.defaultProfile(tenantID, name), nil
		}
		return nil, fmt.Errorf("load profile: %w", err)
	}
	return profile, nil
}

// EncryptBundle builds an encrypted bundle for the given profile + device context.
func (s *Service) EncryptBundle(profile *gormdb.ClientProfile, req EncryptRequest) (string, error) {
	if s.encryptor == nil {
		return "", fmt.Errorf("encryptor not configured")
	}
	if profile == nil {
		return "", ErrProfileNotFound
	}
	if req.Token == "" || req.DeviceID == uuid.Nil || req.TenantID == uuid.Nil {
		return "", ErrInvalidRequest
	}
	bundle := profiles.FromModel(profile).WithContext(req.TenantID, req.DeviceID, req.Platform)
	return s.encryptor.Encrypt(req.Token, bundle)
}

func (s *Service) normalizeProfileName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return s.DefaultProfileName()
	}
	return trimmed
}

func (s *Service) defaultProfile(tenantID uuid.UUID, name string) *gormdb.ClientProfile {
	profile := &gormdb.ClientProfile{
		BaseModel:        gormdb.BaseModel{ID: uuid.New()},
		TenantID:         tenantID,
		ProfileName:      name,
		SamplingRate:     48000,
		NoiseGate:        0.01,
		DetectionToggles: datatypes.JSONMap{"anti_recording": true},
		ModelPresets:     datatypes.JSONMap{"llm": "default"},
		Endpoints:        datatypes.JSONMap{"grpc": "grpc://localhost:9090"},
		Version:          1,
	}
	profile.Signature = s.signProfile(profile)
	return profile
}

func (s *Service) signProfile(profile *gormdb.ClientProfile) []byte {
	if len(s.cfg.SigningKey) == 0 {
		return nil
	}
	mac := hmac.New(sha256.New, s.cfg.SigningKey)
	if profile != nil {
		mac.Write([]byte(profile.ProfileName))
		mac.Write([]byte(fmt.Sprintf("|%d|%d|%f", profile.Version, profile.SamplingRate, profile.NoiseGate)))
		writeJSON(mac, profile.DetectionToggles)
		writeJSON(mac, profile.ModelPresets)
		writeJSON(mac, profile.Endpoints)
	}
	return mac.Sum(nil)
}

func writeJSON(mac hashWriter, value datatypes.JSONMap) {
	if len(value) == 0 {
		return
	}
	data, _ := json.Marshal(value)
	mac.Write(data)
}

type hashWriter interface {
	Write(p []byte) (int, error)
}

func toJSONMap(src map[string]any) datatypes.JSONMap {
	if src == nil {
		return datatypes.JSONMap{}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return datatypes.JSONMap(dst)
}

func (s *Service) publishUpdate(ctx context.Context, tenantID uuid.UUID, profile string, version int) {
	if s.publisher == nil {
		return
	}
	event := events.ConfigUpdateEvent{
		ProfileName: profile,
		Version:     version,
		Metadata:    map[string]any{"timestamp": time.Now().UTC().Format(time.RFC3339Nano)},
	}
	if err := s.publisher.PublishProfileUpdate(ctx, tenantID, event); err != nil && s.logger != nil {
		s.logger.Warn("failed to publish config update", "error", err)
	}
}

func (s *Service) audit(ctx context.Context, entry audit.Entry) {
	if s.auditor == nil {
		return
	}
	s.auditor.Record(ctx, entry)
}
