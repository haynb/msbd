package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/iam"
)

// DeviceHandler exposes pairing + heartbeat REST endpoints.
type DeviceHandler struct {
	authSvc   *iam.Service
	deviceSvc *iam.DeviceService
	logger    *slog.Logger
}

// NewDeviceHandler constructs a DeviceHandler instance.
func NewDeviceHandler(auth *iam.Service, device *iam.DeviceService, logger *slog.Logger) *DeviceHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DeviceHandler{
		authSvc:   auth,
		deviceSvc: device,
		logger:    logger.With("component", "device_http"),
	}
}

// Register wires device routes.
func (h *DeviceHandler) Register(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.Handle("/devices/pairing/request", h.wrap(h.handlePairingRequest))
	mux.Handle("/devices/pairing/approve", h.wrap(h.handlePairingApprove))
	mux.Handle("/devices/pairing/claim", h.wrap(h.handlePairingClaim))
	mux.Handle("/devices/heartbeat", h.wrap(h.handleHeartbeat))
	mux.Handle("/devices/revoke", h.wrap(h.handleRevoke))
}

type deviceHandlerFunc func(http.ResponseWriter, *http.Request) error

func (h *DeviceHandler) wrap(fn deviceHandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			h.writeError(w, r, err)
		}
	})
}

func (h *DeviceHandler) handlePairingRequest(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	var req pairingRequestPayload
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	result, err := h.deviceSvc.RequestPairing(r.Context(), iam.PairingRequest{
		Platform:      req.Platform,
		Fingerprint:   req.Fingerprint,
		ClientVersion: req.ClientVersion,
		DisplayName:   req.DisplayName,
		Metadata:      req.Metadata,
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{
		"code":       result.Code,
		"token":      result.Token,
		"expires_in": int(time.Until(result.ExpiresAt).Seconds()),
	})
}

func (h *DeviceHandler) handlePairingApprove(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	identity, err := h.authorize(r, crypto.TokenKindJWT)
	if err != nil {
		return err
	}
	var req pairingApprovePayload
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	result, err := h.deviceSvc.ApprovePairing(r.Context(), identity, iam.ApprovePairingRequest{
		Code:        req.Code,
		ProfileName: req.ProfileName,
		TrustScore:  req.TrustScore,
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{
		"device_id":      result.DeviceID.String(),
		"status":         result.Status,
		"config_version": result.ConfigVersion,
	})
}

func (h *DeviceHandler) handlePairingClaim(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	var req pairingClaimPayload
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	result, err := h.deviceSvc.ClaimPairing(r.Context(), iam.ClaimPairingRequest{
		Code:  req.Code,
		Token: req.Token,
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{
		"device_id":         result.DeviceID.String(),
		"config_version":    result.ConfigVersion,
		"encrypted_profile": result.EncryptedProfile,
	})
}

func (h *DeviceHandler) handleHeartbeat(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	identity, err := h.authorize(r, crypto.TokenKindPASETO)
	if err != nil {
		return err
	}
	var req heartbeatPayload
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return errBadRequest
	}
	if err := h.deviceSvc.ReportHeartbeat(r.Context(), identity, iam.HeartbeatRequest{
		DeviceID: deviceID,
		Status:   req.Status,
		Metrics:  req.Metrics,
	}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusAccepted, map[string]any{"accepted": true})
}

func (h *DeviceHandler) handleRevoke(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	identity, err := h.authorize(r, crypto.TokenKindJWT)
	if err != nil {
		return err
	}
	var req revokePayload
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	deviceID, err := uuid.Parse(req.DeviceID)
	if err != nil {
		return errBadRequest
	}
	if err := h.deviceSvc.RevokeDevice(r.Context(), identity, iam.RevokeDeviceRequest{
		DeviceID: deviceID,
		Reason:   req.Reason,
	}); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

func (h *DeviceHandler) authorize(r *http.Request, kind crypto.TokenKind) (iam.DeviceIdentity, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return iam.DeviceIdentity{}, iam.ErrInvalidCredentials
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return iam.DeviceIdentity{}, iam.ErrInvalidCredentials
	}
	claims, err := h.authSvc.Validate(r.Context(), parts[1], kind)
	if err != nil {
		return iam.DeviceIdentity{}, err
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return iam.DeviceIdentity{}, iam.ErrInvalidCredentials
	}
	tenantID, err := uuid.Parse(claims.TenantID)
	if err != nil {
		return iam.DeviceIdentity{}, iam.ErrInvalidCredentials
	}
	return iam.DeviceIdentity{
		UserID:   userID,
		TenantID: tenantID,
		Email:    claims.Email,
	}, nil
}

func (h *DeviceHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
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
	case errors.Is(err, iam.ErrAccountFrozen):
		code = http.StatusForbidden
		appCode = "40301"
		message = "account frozen"
	case errors.Is(err, iam.ErrPairingCodeConflict):
		code = http.StatusConflict
		appCode = "40901"
		message = "pairing code conflict"
	case errors.Is(err, iam.ErrPairingNotFound):
		code = http.StatusNotFound
		appCode = "40404"
		message = "pairing not found"
	case errors.Is(err, iam.ErrPairingExpired):
		code = http.StatusGone
		appCode = "41001"
		message = "pairing expired"
	case errors.Is(err, iam.ErrPairingPending):
		code = http.StatusConflict
		appCode = "40902"
		message = "pairing pending"
	case errors.Is(err, iam.ErrPairingTokenMismatch):
		code = http.StatusForbidden
		appCode = "40302"
		message = "pairing token mismatch"
	case errors.Is(err, iam.ErrDeviceNotFound):
		code = http.StatusNotFound
		appCode = "40405"
		message = "device not found"
	case errors.Is(err, iam.ErrInvalidHeartbeatStatus):
		code = http.StatusBadRequest
		appCode = "40002"
		message = "invalid heartbeat status"
	default:
		h.logger.Error("device handler error", "error", err)
	}
	_ = writeJSON(w, code, errorResponse{
		Code:      appCode,
		Message:   message,
		RequestID: r.Header.Get("X-Request-ID"),
	})
}

type pairingRequestPayload struct {
	Platform      string         `json:"platform"`
	Fingerprint   string         `json:"fingerprint"`
	ClientVersion string         `json:"client_version"`
	DisplayName   string         `json:"display_name"`
	Metadata      map[string]any `json:"metadata"`
}

type pairingApprovePayload struct {
	Code        string `json:"code"`
	ProfileName string `json:"profile_name"`
	TrustScore  int    `json:"trust_score"`
}

type pairingClaimPayload struct {
	Code  string `json:"code"`
	Token string `json:"token"`
}

type heartbeatPayload struct {
	DeviceID string         `json:"device_id"`
	Status   string         `json:"status"`
	Metrics  map[string]any `json:"metrics"`
}

type revokePayload struct {
	DeviceID string `json:"device_id"`
	Reason   string `json:"reason"`
}
