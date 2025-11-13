package gormdb

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// BaseModel embeds common audit fields (UUID primary key + timestamps).
type BaseModel struct {
	ID        uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// Tenant represents an organization boundary.
type Tenant struct {
	BaseModel
	Name            string            `gorm:"uniqueIndex"`
	Tier            string            `gorm:"type:tier_enum;default:'standard'"`
	DefaultPolicies datatypes.JSONMap `gorm:"type:jsonb"`
	BillingMeta     datatypes.JSONMap `gorm:"type:jsonb"`
}

// User is a member belonging to a tenant.
type User struct {
	BaseModel
	TenantID     uuid.UUID      `gorm:"type:uuid;index"`
	Email        string         `gorm:"type:citext;uniqueIndex"`
	PasswordHash []byte         `gorm:"type:bytea"`
	Roles        pq.StringArray `gorm:"type:text[]"`
	State        string         `gorm:"type:state_enum;default:'active'"`
	LastLogin    time.Time      `gorm:""`
}

// PolicyRule models RBAC/ABAC rules.
type PolicyRule struct {
	BaseModel
	TenantID   uuid.UUID `gorm:"type:uuid;index"`
	Name       string
	Effect     string
	Actions    pq.StringArray    `gorm:"type:text[]"`
	Resources  pq.StringArray    `gorm:"type:text[]"`
	Conditions datatypes.JSONMap `gorm:"type:jsonb"`
}

// Group defines quotas and roles applied to a subset of users.
type Group struct {
	BaseModel
	TenantID           uuid.UUID `gorm:"type:uuid;index"`
	Name               string
	Roles              pq.StringArray `gorm:"type:text[]"`
	SpeechQuotaSeconds int64
	LlmTokenQuota      int64
	ScreenshotQuota    int64
	OverdraftPolicy    datatypes.JSONMap `gorm:"type:jsonb"`
	UpdatedBy          uuid.UUID         `gorm:"type:uuid"`
}

// QuotaSnapshot captures usage at a point in time.
type QuotaSnapshot struct {
	BaseModel
	TenantID          uuid.UUID `gorm:"type:uuid;index"`
	UserID            uuid.UUID `gorm:"type:uuid;index"`
	SpeechSecondsUsed int64
	LLMTokensUsed     int64
	ScreenshotsUsed   int64
	State             string `gorm:"type:state_enum;default:'active'"`
	CapturedAt        time.Time
}

// Device tracks client trust + status.
type Device struct {
	BaseModel
	UserID        uuid.UUID `gorm:"type:uuid;index"`
	TenantID      uuid.UUID `gorm:"type:uuid;index"`
	Platform      string    `gorm:"type:platform_enum;default:'unknown'"`
	Fingerprint   string
	TrustScore    int16
	Status        string `gorm:"type:device_status_enum;default:'pending'"`
	LastSeen      time.Time
	ConfigVersion int
	Metadata      datatypes.JSONMap `gorm:"type:jsonb"`
}

// ClientProfile defines signed config bundles for desktop clients.
type ClientProfile struct {
	BaseModel
	TenantID         uuid.UUID `gorm:"type:uuid;index"`
	ProfileName      string
	SamplingRate     int
	NoiseGate        float32
	DetectionToggles datatypes.JSONMap `gorm:"type:jsonb"`
	ModelPresets     datatypes.JSONMap `gorm:"type:jsonb"`
	Endpoints        datatypes.JSONMap `gorm:"type:jsonb"`
	Version          int
	Signature        []byte `gorm:"type:bytea"`
}

// AuditLog is an immutable compliance trail.
type AuditLog struct {
	BaseModel
	EventID   uuid.UUID `gorm:"type:uuid;uniqueIndex"`
	TenantID  uuid.UUID `gorm:"type:uuid;index"`
	ActorID   uuid.UUID `gorm:"type:uuid"`
	Action    string
	Result    string
	LatencyMs int
	Geo       string
	Metadata  datatypes.JSONMap `gorm:"type:jsonb"`
}
