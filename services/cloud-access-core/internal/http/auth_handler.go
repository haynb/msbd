package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/iam"
)

// AuthHandler exposes REST endpoints for login/refresh/logout flows.
type AuthHandler struct {
	service *iam.Service
	logger  *slog.Logger
}

// NewAuthHandler constructs the handler.
func NewAuthHandler(service *iam.Service, logger *slog.Logger) *AuthHandler {
	return &AuthHandler{service: service, logger: logger.With("component", "auth_http")}
}

// Register wires the HTTP routes into the mux.
func (h *AuthHandler) Register(mux *http.ServeMux) {
	mux.Handle("/auth/login", h.wrap(h.handleLogin))
	mux.Handle("/auth/refresh", h.wrap(h.handleRefresh))
	mux.Handle("/auth/logout", h.wrap(h.handleLogout))
}

type handlerFunc func(http.ResponseWriter, *http.Request) error

func (h *AuthHandler) wrap(fn handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			h.writeError(w, r, err)
		}
	})
}

func (h *AuthHandler) handleLogin(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	var req loginRequest
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	result, err := h.service.Login(r.Context(), iam.LoginRequest{
		Email:             req.Email,
		Password:          req.Password,
		ClientKind:        req.ClientKind,
		DeviceFingerprint: req.DeviceFingerprint,
		ClientIP:          clientIP(r),
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, tokenResponseFromResult(result))
}

func (h *AuthHandler) handleRefresh(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	var req refreshRequest
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	result, err := h.service.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, tokenResponseFromResult(result))
}

func (h *AuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	var req refreshRequest
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.service.Logout(r.Context(), req.RefreshToken); err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
}

func (h *AuthHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
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
	case errors.Is(err, iam.ErrSessionExpired):
		code = http.StatusUnauthorized
		appCode = "40102"
		message = "session expired"
	case errors.Is(err, iam.ErrSessionNotFound):
		code = http.StatusUnauthorized
		appCode = "40103"
		message = "session not found"
	default:
		h.logger.Error("auth handler error", "error", err)
	}
	_ = writeJSON(w, code, errorResponse{
		Code:      appCode,
		Message:   message,
		RequestID: r.Header.Get("X-Request-ID"),
	})
}

type loginRequest struct {
	Email             string `json:"email"`
	Password          string `json:"password"`
	ClientKind        string `json:"client_kind"`
	DeviceFingerprint string `json:"device_fingerprint"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenResponse struct {
	AccessToken      string   `json:"access_token"`
	DeviceToken      string   `json:"device_token"`
	RefreshToken     string   `json:"refresh_token"`
	ExpiresIn        int64    `json:"expires_in"`
	RefreshExpiresIn int64    `json:"refresh_expires_in"`
	SessionID        string   `json:"session_id"`
	TokenType        string   `json:"token_type"`
	TenantID         string   `json:"tenant_id"`
	UserID           string   `json:"user_id"`
	Roles            []string `json:"roles"`
}

type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func tokenResponseFromResult(result *iam.AuthResult) tokenResponse {
	accessIn := time.Until(result.AccessExpiresAt).Seconds()
	refreshIn := time.Until(result.RefreshExpiresAt).Seconds()
	if accessIn < 0 {
		accessIn = 0
	}
	if refreshIn < 0 {
		refreshIn = 0
	}
	return tokenResponse{
		AccessToken:      result.AccessToken,
		DeviceToken:      result.DeviceToken,
		RefreshToken:     result.RefreshToken,
		ExpiresIn:        int64(accessIn),
		RefreshExpiresIn: int64(refreshIn),
		SessionID:        result.SessionID,
		TokenType:        "Bearer",
		TenantID:         result.TenantID.String(),
		UserID:           result.UserID.String(),
		Roles:            result.Roles,
	}
}
