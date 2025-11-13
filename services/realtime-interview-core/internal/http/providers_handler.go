package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/authn"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/speechgateway"
)

// ProviderReader exposes the subset of gateway methods needed for HTTP.
type ProviderReader interface {
	Status() speechgateway.GatewayStatus
}

// ProviderHandler reports provider health + config.
type ProviderHandler struct {
	gateway ProviderReader
	store   *speechgateway.ConfigStore
	logger  *slog.Logger
}

// NewProviderHandler builds the handler.
func NewProviderHandler(gateway ProviderReader, store *speechgateway.ConfigStore, logger *slog.Logger) *ProviderHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProviderHandler{gateway: gateway, store: store, logger: logger.With("component", "providers_http")}
}

// Register attaches routes to the router.
func (h *ProviderHandler) Register(router chi.Router) {
	router.Get("/providers", h.handleStatus)
	if h.store != nil {
		router.Get("/providers/runtime", h.wrap(h.handleRuntimeGet))
		router.Put("/providers/runtime", h.wrap(h.handleRuntimeUpdate))
	}
}

func (h *ProviderHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := h.gateway.Status()
	if err := writeJSON(w, http.StatusOK, map[string]any{"gateway": status}); err != nil && h.logger != nil {
		h.logger.Warn("write provider status failed", "error", err)
	}
}

func (h *ProviderHandler) handleRuntimeGet(w http.ResponseWriter, r *http.Request) error {
	if h.store == nil {
		return errRuntimeDisabled
	}
	if err := h.ensureOps(r); err != nil {
		return err
	}
	cfg, err := h.store.Load(r.Context())
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, map[string]any{"runtime": cfg})
}

func (h *ProviderHandler) handleRuntimeUpdate(w http.ResponseWriter, r *http.Request) error {
	if h.store == nil {
		return errRuntimeDisabled
	}
	if err := h.ensureOps(r); err != nil {
		return err
	}
	var payload speechgateway.RuntimeConfig
	if err := parseJSON(w, r, &payload); err != nil {
		return err
	}
	if err := h.store.Save(r.Context(), payload); err != nil {
		return err
	}
	return writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

type providerHandlerFunc func(http.ResponseWriter, *http.Request) error

func (h *ProviderHandler) wrap(fn providerHandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			h.writeError(w, err)
		}
	}
}

var (
	errUnauthorized    = errors.New("unauthorized")
	errForbidden       = errors.New("forbidden")
	errRuntimeDisabled = errors.New("runtime config store unavailable")
)

func (h *ProviderHandler) ensureOps(r *http.Request) error {
	claims, ok := authn.ClaimsFromContext(r.Context())
	if !ok {
		return errUnauthorized
	}
	for _, role := range claims.Roles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "admin" || role == "ops" {
			return nil
		}
	}
	return errForbidden
}

func (h *ProviderHandler) writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, errUnauthorized):
		status = http.StatusUnauthorized
	case errors.Is(err, errForbidden):
		status = http.StatusForbidden
	case errors.Is(err, errBadRequest):
		status = http.StatusBadRequest
	case errors.Is(err, errRuntimeDisabled):
		status = http.StatusServiceUnavailable
	default:
		// fallthrough to 500
	}
	_ = writeJSON(w, status, map[string]any{"error": err.Error()})
}
