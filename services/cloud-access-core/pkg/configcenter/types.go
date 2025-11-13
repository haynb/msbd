package configcenter

import (
	"context"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
)

// Repository describes the storage subset required by the config center service.
type Repository interface {
	ClientProfileByName(ctx context.Context, tenantID uuid.UUID, name string) (*gormdb.ClientProfile, error)
	SaveClientProfile(ctx context.Context, profile *gormdb.ClientProfile) error
}

// ProfileSpec captures the incoming payload for profile creation/update.
type ProfileSpec struct {
	TenantID     uuid.UUID
	ProfileName  string
	SamplingRate int
	NoiseGate    float32
	Detection    map[string]any
	ModelPresets map[string]any
	Endpoints    map[string]any
	ActorID      uuid.UUID
}

// EncryptRequest contains the context for building encrypted bundles.
type EncryptRequest struct {
	TenantID uuid.UUID
	DeviceID uuid.UUID
	Platform string
	Token    string
}
