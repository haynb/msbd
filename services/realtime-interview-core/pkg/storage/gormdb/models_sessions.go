package gormdb

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// BaseModel embeds UUID PK + timestamps for all tables.
type BaseModel struct {
	ID        uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	CreatedAt time.Time      `gorm:"autoCreateTime"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// Session tracks a realtime interview lifecycle.
type Session struct {
	BaseModel
	TenantID      uuid.UUID         `gorm:"type:uuid;index"`
	UserID        uuid.UUID         `gorm:"type:uuid;index"`
	DeviceID      *uuid.UUID        `gorm:"type:uuid"`
	RequestKey    string            `gorm:"column:request_key"`
	State         string            `gorm:"type:session_state_enum;index"`
	Mode          string            `gorm:"type:session_mode_enum"`
	StartedAt     time.Time         `gorm:"autoCreateTime"`
	EndedAt       *time.Time        `gorm:""`
	LastHeartbeat time.Time         `gorm:""`
	Metadata      datatypes.JSONMap `gorm:"type:jsonb"`
}

// SessionParticipant captures active participants within a session.
type SessionParticipant struct {
	BaseModel
	SessionID uuid.UUID `gorm:"type:uuid;index"`
	UserID    uuid.UUID `gorm:"type:uuid"`
	Role      string    `gorm:"type:session_role_enum"`
	JoinedAt  time.Time
	LeftAt    *time.Time
}

// SessionSegment stores incremental transcripts emitted by a provider.
type SessionSegment struct {
	BaseModel
	SessionID             uuid.UUID         `gorm:"type:uuid;index"`
	Sequence              int               `gorm:""`
	Transcript            datatypes.JSONMap `gorm:"type:jsonb"`
	Confidence            float32
	ProviderLatencyMillis int
}

// SessionUsage aggregates duration + tokens for billing.
type SessionUsage struct {
	BaseModel
	SessionID     uuid.UUID `gorm:"type:uuid;index"`
	TenantID      uuid.UUID `gorm:"type:uuid;index"`
	SpeechSeconds int
	LLMTokens     int
	ErrorCount    int
	WindowStart   time.Time
	WindowEnd     time.Time
}

// SpeechProviderMetric records provider health + latency signals.
type SpeechProviderMetric struct {
	BaseModel
	SessionID  *uuid.UUID `gorm:"type:uuid;index"`
	Provider   string
	EventType  string
	Value      datatypes.JSONMap `gorm:"type:jsonb"`
	RecordedAt time.Time
}
