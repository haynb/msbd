package gormdb

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository exposes typed helpers for working with GORM.
type Repository struct {
	db *gorm.DB
}

// ErrNotFound mirrors gorm.ErrRecordNotFound.
var ErrNotFound = gorm.ErrRecordNotFound

// NewRepository constructs a new Repository wrapper.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// DB returns the underlying gorm DB.
func (r *Repository) DB() *gorm.DB {
	return r.db
}

// WithTransaction executes fn within a transaction.
func (r *Repository) WithTransaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

// CreateTenant persists a tenant.
func (r *Repository) CreateTenant(ctx context.Context, tenant *Tenant) error {
	return r.db.WithContext(ctx).Create(tenant).Error
}

// TenantByID fetches a tenant by uuid.
func (r *Repository) TenantByID(ctx context.Context, id uuid.UUID) (*Tenant, error) {
	var tenant Tenant
	if err := r.db.WithContext(ctx).First(&tenant, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &tenant, nil
}

// CreateUser persists a user.
func (r *Repository) CreateUser(ctx context.Context, user *User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

// ListPolicyRules returns all policy rules for a tenant.
func (r *Repository) ListPolicyRules(ctx context.Context, tenantID uuid.UUID) ([]PolicyRule, error) {
	var rules []PolicyRule
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Order("name ASC").Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

// ReplacePolicyRules replaces the existing rules for a tenant with the provided slice.
func (r *Repository) ReplacePolicyRules(ctx context.Context, tenantID uuid.UUID, rules []*PolicyRule) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ?", tenantID).Delete(&PolicyRule{}).Error; err != nil {
			return err
		}
		if len(rules) == 0 {
			return nil
		}
		return tx.Create(&rules).Error
	})
}

// UserByEmail finds a user by email.
func (r *Repository) UserByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// UserByID returns a user by UUID primary key.
func (r *Repository) UserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).First(&user, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// UpsertGroup inserts or updates a group record.
func (r *Repository) UpsertGroup(ctx context.Context, group *Group) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"roles", "speech_quota_seconds", "llm_token_quota", "screenshot_quota", "overdraft_policy", "updated_by", "updated_at"}),
	}).Create(group).Error
}

// GroupsByTenant returns all active groups for a tenant.
func (r *Repository) GroupsByTenant(ctx context.Context, tenantID uuid.UUID) ([]Group, error) {
	var groups []Group
	if err := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID).Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

// RecordQuotaSnapshot stores a quota snapshot.
func (r *Repository) RecordQuotaSnapshot(ctx context.Context, snapshot *QuotaSnapshot) error {
	return r.db.WithContext(ctx).Create(snapshot).Error
}

// SaveDevice inserts or updates a device row keyed by fingerprint.
func (r *Repository) SaveDevice(ctx context.Context, device *Device) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "last_seen", "config_version", "metadata", "updated_at", "trust_score"}),
	}).Create(device).Error
}

// DeviceByID returns a device by its UUID primary key.
func (r *Repository) DeviceByID(ctx context.Context, id uuid.UUID) (*Device, error) {
	var device Device
	if err := r.db.WithContext(ctx).First(&device, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &device, nil
}

// UpdateDeviceHeartbeat updates device last seen + status metadata without requiring full load.
func (r *Repository) UpdateDeviceHeartbeat(ctx context.Context, deviceID uuid.UUID, status string, lastSeen time.Time, metadata map[string]any) error {
	update := map[string]any{
		"status":     status,
		"last_seen":  lastSeen,
		"updated_at": time.Now().UTC(),
	}
	if metadata != nil {
		update["metadata"] = datatypes.JSONMap(metadata)
	}
	return r.db.WithContext(ctx).Model(&Device{}).Where("id = ?", deviceID).Updates(update).Error
}

// ClientProfileByName fetches a profile by tenant + name.
func (r *Repository) ClientProfileByName(ctx context.Context, tenantID uuid.UUID, name string) (*ClientProfile, error) {
	var profile ClientProfile
	if err := r.db.WithContext(ctx).Where("tenant_id = ? AND profile_name = ?", tenantID, name).First(&profile).Error; err != nil {
		return nil, err
	}
	return &profile, nil
}

// SaveClientProfile upserts a profile.
func (r *Repository) SaveClientProfile(ctx context.Context, profile *ClientProfile) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "profile_name"}},
		DoUpdates: clause.AssignmentColumns([]string{"sampling_rate", "noise_gate", "detection_toggles", "model_presets", "endpoints", "version", "signature", "updated_at"}),
	}).Create(profile).Error
}

// RecordAuditLog appends an audit entry.
func (r *Repository) RecordAuditLog(ctx context.Context, logEntry *AuditLog) error {
	return r.db.WithContext(ctx).Create(logEntry).Error
}

// EnsureTenantExists returns tenant if exists or ErrNotFound.
func (r *Repository) EnsureTenantExists(ctx context.Context, id uuid.UUID) error {
	var count int64
	if err := r.db.WithContext(ctx).Model(&Tenant{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUserState updates the state flag for a user.
func (r *Repository) UpdateUserState(ctx context.Context, id uuid.UUID, state string) error {
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("state", state).Error
}

// AutoMigrate applies GORM migrations for all models (used in tests/tools).
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&Tenant{},
		&User{},
		&PolicyRule{},
		&Group{},
		&QuotaSnapshot{},
		&Device{},
		&ClientProfile{},
		&AuditLog{},
	)
}

// IsUniqueViolation helper for constraint conflicts.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, gorm.ErrDuplicatedKey)
}
