package profiles

import (
	"time"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
)

// Bundle represents the payload delivered to desktop clients.
type Bundle struct {
	ProfileName    string         `json:"profile_name"`
	Version        int            `json:"version"`
	SamplingRate   int            `json:"sampling_rate"`
	NoiseGate      float32        `json:"noise_gate"`
	Detection      map[string]any `json:"detection"`
	ModelPresets   map[string]any `json:"model_presets"`
	Endpoints      map[string]any `json:"endpoints"`
	IssuedAt       time.Time      `json:"issued_at"`
	TenantID       string         `json:"tenant_id"`
	DeviceID       string         `json:"device_id"`
	DevicePlatform string         `json:"device_platform"`
}

// FromModel copies the relevant profile fields into a bundle skeleton.
func FromModel(profile *gormdb.ClientProfile) Bundle {
	if profile == nil {
		return Bundle{}
	}
	return Bundle{
		ProfileName:  profile.ProfileName,
		Version:      profile.Version,
		SamplingRate: profile.SamplingRate,
		NoiseGate:    profile.NoiseGate,
		Detection:    CloneJSON(profile.DetectionToggles),
		ModelPresets: CloneJSON(profile.ModelPresets),
		Endpoints:    CloneJSON(profile.Endpoints),
	}
}

// WithContext applies runtime metadata onto the bundle.
func (b Bundle) WithContext(tenantID uuid.UUID, deviceID uuid.UUID, platform string) Bundle {
	b.TenantID = tenantID.String()
	b.DeviceID = deviceID.String()
	b.DevicePlatform = platform
	b.IssuedAt = time.Now().UTC()
	return b
}
