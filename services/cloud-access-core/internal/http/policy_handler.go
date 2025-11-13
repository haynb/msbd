package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/policy"
)

// PolicyHandler exposes admin + evaluation routes for policy service.
type PolicyHandler struct {
	service *policy.Service
	logger  *slog.Logger
}

// NewPolicyHandler constructs a PolicyHandler instance.
func NewPolicyHandler(service *policy.Service, logger *slog.Logger) *PolicyHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &PolicyHandler{service: service, logger: logger.With("component", "policy_http")}
}

// Register attaches policy routes to the mux.
func (h *PolicyHandler) Register(mux *http.ServeMux) {
	if mux == nil {
		return
	}
	mux.Handle("/policies", h.wrap(h.handlePolicies))
	mux.Handle("/policies/check", h.wrap(h.handleCheckAccess))
	mux.Handle("/policies/evaluate", h.wrap(h.handleEvaluateQuota))
	mux.Handle("/groups", h.wrap(h.handleGroupUpsert))
	mux.Handle("/tenants", h.wrap(h.handleTenantCreate))
}

type policyHandlerFunc func(http.ResponseWriter, *http.Request) error

func (h *PolicyHandler) wrap(fn policyHandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			h.writeError(w, r, err)
		}
	})
}

func (h *PolicyHandler) handlePolicies(w http.ResponseWriter, r *http.Request) error {
	switch r.Method {
	case http.MethodGet:
		tenantParam := r.URL.Query().Get("tenant_id")
		tenantID, err := uuid.Parse(tenantParam)
		if err != nil {
			return errBadRequest
		}
		rules, err := h.service.ListPolicyRules(r.Context(), tenantID)
		if err != nil {
			return err
		}
		return writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
	case http.MethodPut:
		var req policySetPayload
		if err := parseJSON(w, r, &req); err != nil {
			return err
		}
		tenantID, err := uuid.Parse(req.TenantID)
		if err != nil {
			return errBadRequest
		}
		rules, err := h.service.SavePolicyRules(r.Context(), tenantID, req.Rules)
		if err != nil {
			return err
		}
		return writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
}

func (h *PolicyHandler) handleCheckAccess(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	var req checkAccessPayload
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		return errBadRequest
	}
	var userID uuid.UUID
	if req.UserID != "" {
		userID, err = uuid.Parse(req.UserID)
		if err != nil {
			return errBadRequest
		}
	}
	decision, err := h.service.CheckAccess(r.Context(), policy.CheckAccessRequest{
		TenantID: tenantID,
		UserID:   userID,
		Roles:    req.Roles,
		Action:   req.Action,
		Resource: req.Resource,
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, decision)
}

func (h *PolicyHandler) handleEvaluateQuota(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	var req quotaPayload
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	var tenantID uuid.UUID
	var err error
	if req.TenantID != "" {
		tenantID, err = uuid.Parse(req.TenantID)
		if err != nil {
			return errBadRequest
		}
	}
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		return errBadRequest
	}
	decision, err := h.service.EvaluateQuota(r.Context(), policy.QuotaRequest{
		TenantID: tenantID,
		UserID:   userID,
		Roles:    req.Roles,
		Resource: req.Resource,
		Amount:   req.Amount,
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, decision)
}

func (h *PolicyHandler) handleGroupUpsert(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	var req groupPayload
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		return errBadRequest
	}
	var updatedBy uuid.UUID
	if req.UpdatedBy != "" {
		updatedBy, err = uuid.Parse(req.UpdatedBy)
		if err != nil {
			return errBadRequest
		}
	}
	group, err := h.service.UpsertGroup(r.Context(), policy.GroupRequest{
		TenantID:        tenantID,
		Name:            req.Name,
		Roles:           req.Roles,
		SpeechQuota:     req.SpeechQuota,
		LLMTokensQuota:  req.LLMTokensQuota,
		ScreenshotQuota: req.ScreenshotQuota,
		UpdatedBy:       updatedBy,
		OverdraftPolicy: req.OverdraftPolicy,
	})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusOK, group)
}

func (h *PolicyHandler) handleTenantCreate(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil
	}
	var req tenantPayload
	if err := parseJSON(w, r, &req); err != nil {
		return err
	}
	tenant, err := h.service.CreateTenant(r.Context(), policy.TenantRequest{Name: req.Name, Tier: req.Tier})
	if err != nil {
		return err
	}
	return writeJSON(w, http.StatusCreated, tenant)
}

func (h *PolicyHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	code := http.StatusInternalServerError
	appCode := "50000"
	message := "internal error"

	switch {
	case errors.Is(err, errBadRequest):
		code = http.StatusBadRequest
		appCode = "40001"
		message = "invalid request"
	case errors.Is(err, policy.ErrInvalidRequest):
		code = http.StatusBadRequest
		appCode = "40001"
		message = "invalid policy request"
	case errors.Is(err, policy.ErrTenantNotFound):
		code = http.StatusNotFound
		appCode = "40401"
		message = "tenant not found"
	default:
		if h.logger != nil {
			h.logger.Error("policy handler error", "error", err, "path", r.URL.Path)
		}
	}

	_ = writeJSON(w, code, errorResponse{Code: appCode, Message: message, RequestID: r.Header.Get("X-Request-ID")})
}

type policySetPayload struct {
	TenantID string            `json:"tenant_id"`
	Rules    []policy.RuleSpec `json:"rules"`
}

type checkAccessPayload struct {
	TenantID string   `json:"tenant_id"`
	UserID   string   `json:"user_id"`
	Roles    []string `json:"roles"`
	Action   string   `json:"action"`
	Resource string   `json:"resource"`
}

type quotaPayload struct {
	TenantID string   `json:"tenant_id"`
	UserID   string   `json:"user_id"`
	Roles    []string `json:"roles"`
	Resource string   `json:"resource"`
	Amount   int64    `json:"amount"`
}

type groupPayload struct {
	TenantID        string         `json:"tenant_id"`
	Name            string         `json:"name"`
	Roles           []string       `json:"roles"`
	SpeechQuota     int64          `json:"speech_quota"`
	LLMTokensQuota  int64          `json:"llm_quota"`
	ScreenshotQuota int64          `json:"screenshot_quota"`
	UpdatedBy       string         `json:"updated_by"`
	OverdraftPolicy map[string]any `json:"overdraft_policy"`
}

type tenantPayload struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}
