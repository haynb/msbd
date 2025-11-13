package httpapi

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/configcenter"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/iam"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
)

// ConfigHandler exposes profile CRUD and fetch endpoints.
type ConfigHandler struct {
	auth    *iam.Service
	devices DeviceRepository
	service *configcenter.Service
	logger  *slog.Logger
}

// DeviceRepository defines the subset of repository operations needed by the handler.
type DeviceRepository interface {
	DeviceByID(ctx context.Context, id uuid.UUID) (*gormdb.Device, error)
}

// NewConfigHandler constructs a ConfigHandler.
func NewConfigHandler(auth *iam.Service, devices DeviceRepository, service *configcenter.Service, logger *slog.Logger) *ConfigHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ConfigHandler{auth: auth, devices: devices, service: service, logger: logger.With("component", "config_http")}
}

// Register attaches config routes.
func (h *ConfigHandler) Register(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.Handle("/configs/profile", h.wrap(h.handleProfileFetch))
	mux.Handle("/configs/profiles", h.wrap(h.handleProfiles))
}

type configHandlerFunc func(http.ResponseWriter, *http.Request) error

func (h *ConfigHandler) wrap(fn configHandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			h.writeError(w, r, err)
		}
	})
}

func (h *ConfigHandler) handleProfiles(w http.ResponseWriter, r *http.Request) error {
	switch r.Method {
	case http.MethodPut:
		return h.handleProfileUpsert(w, r)
	case http.MethodGet:
		return h.handleProfileDescribe(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
}

func (h *ConfigHandler) handleProfileFetch(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	claims, err := h.authorize(r, crypto.TokenKindPASETO)
	if err != nil {
		return err
	}
	tenantID, err := uuid.Parse(claims.TenantID)
	if err != nil {
		return iam.ErrInvalidCredentials
	}
	deviceIDParam := r.URL.Query().Get("device_id")
	deviceID, err := uuid.Parse(deviceIDParam)
	if err != nil {
		return errBadRequest
	}
	device, err := h.devices.DeviceByID(r.Context(), deviceID)
	if err != nil {
		if errors.Is(err, gormdb.ErrNotFound) {
			return iam.ErrDeviceNotFound
		}
		return err
	}
	if device.TenantID != tenantID {
		return iam.ErrInvalidCredentials
	}
	profileName := r.URL.Query().Get("profile")
	profile, err := h.service.GetProfile(r.Context(), tenantID, profileName)
	if err != nil {
		return err
	}
	encrypted, err := h.service.EncryptBundle(profile, configcenter.EncryptRequest{
		TenantID: tenantID,
		DeviceID: device.ID,
		Platform: device.Platform,
		Token:    claims.SessionID,
	})
	if err != nil {
		return err
	}
	signature := base64.StdEncoding.EncodeToString(profile.Signature)
	return writeJSON(w, http.StatusOK, map[string]any{
		"profile_name":      profile.ProfileName,
		"version":           profile.Version,
		"encrypted_profile": encrypted,
		"signature":         signature,
	})
}

func (h *ConfigHandler) handleProfileUpsert(w http.ResponseWriter, r *http.Request) error {
	claims, err := h.authorize(r, crypto.TokenKindJWT)
	if err != nil {
		return err
	}
	if !hasAdminRole(claims.Roles) {
		return iam.ErrInvalidCredentials
	}
	var payload profileUpsertPayload
	if err := parseJSON(w, r, &payload); err != nil {
		return err
	}
	tenantID, err := uuid.Parse(payload.TenantID)
	if err != nil {
		return errBadRequest
	}
	actorID, err := uuid.Parse(claims.Subject)
	if err != nil {
		actorID = uuid.Nil
	}
	profile, err := h.service.UpsertProfile(r.Context(), configcenter.ProfileSpec{
		TenantID:     tenantID,
		ProfileName:  payload.ProfileName,
		SamplingRate: payload.SamplingRate,
		NoiseGate:    payload.NoiseGate,
		Detection:    payload.Detection,
		ModelPresets: payload.ModelPresets,
		Endpoints:    payload.Endpoints,
		ActorID:      actorID,
	})
	if err != nil {
		return err
	}
	signature := base64.StdEncoding.EncodeToString(profile.Signature)
	return writeJSON(w, http.StatusOK, map[string]any{
		"profile_name": profile.ProfileName,
		"version":      profile.Version,
		"signature":    signature,
	})
}

func (h *ConfigHandler) handleProfileDescribe(w http.ResponseWriter, r *http.Request) error {
	claims, err := h.authorize(r, crypto.TokenKindJWT)
	if err != nil {
		return err
	}
	if !hasAdminRole(claims.Roles) {
		return iam.ErrInvalidCredentials
	}
	tenantIDParam := r.URL.Query().Get("tenant_id")
	tenantID, err := uuid.Parse(tenantIDParam)
	if err != nil {
		return errBadRequest
	}
	profileName := r.URL.Query().Get("profile")
	profile, err := h.service.GetProfile(r.Context(), tenantID, profileName)
	if err != nil {
		return err
	}
	signature := base64.StdEncoding.EncodeToString(profile.Signature)
	return writeJSON(w, http.StatusOK, map[string]any{
		"profile_name":  profile.ProfileName,
		"sampling_rate": profile.SamplingRate,
		"noise_gate":    profile.NoiseGate,
		"version":       profile.Version,
		"signature":     signature,
		"detection":     profile.DetectionToggles,
		"model_presets": profile.ModelPresets,
		"endpoints":     profile.Endpoints,
	})
}

func (h *ConfigHandler) authorize(r *http.Request, kind crypto.TokenKind) (*crypto.Claims, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return nil, iam.ErrInvalidCredentials
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, iam.ErrInvalidCredentials
	}
	claims, err := h.auth.Validate(r.Context(), parts[1], kind)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func (h *ConfigHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := http.StatusInternalServerError
	appCode := "50000"
	message := "internal error"
	switch {
	case errors.Is(err, errBadRequest):
		code = http.StatusBadRequest
		appCode = "40001"
		message = "invalid request"
	case errors.Is(err, iam.ErrInvalidCredentials):
		code = http.StatusUnauthorized
		appCode = "40101"
		message = "invalid credentials"
	case errors.Is(err, iam.ErrDeviceNotFound):
		code = http.StatusNotFound
		appCode = "40405"
		message = "device not found"
	case errors.Is(err, configcenter.ErrProfileNotFound):
		code = http.StatusNotFound
		appCode = "40406"
		message = "profile not found"
	case errors.Is(err, configcenter.ErrInvalidRequest):
		code = http.StatusBadRequest
		appCode = "40001"
		message = "invalid request"
	default:
		if h.logger != nil {
			h.logger.Error("config handler error", "error", err, "path", r.URL.Path)
		}
	}
	_ = writeJSON(w, code, errorResponse{Code: appCode, Message: message, RequestID: r.Header.Get("X-Request-ID")})
}

func hasAdminRole(roles []string) bool {
	for _, role := range roles {
		if strings.EqualFold(role, "admin") || strings.EqualFold(role, "super_admin") {
			return true
		}
	}
	return false
}

type profileUpsertPayload struct {
	TenantID     string         `json:"tenant_id"`
	ProfileName  string         `json:"profile_name"`
	SamplingRate int            `json:"sampling_rate"`
	NoiseGate    float32        `json:"noise_gate"`
	Detection    map[string]any `json:"detection"`
	ModelPresets map[string]any `json:"model_presets"`
	Endpoints    map[string]any `json:"endpoints"`
}
