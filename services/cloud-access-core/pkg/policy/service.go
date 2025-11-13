package policy

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/open-policy-agent/opa/rego"
	"gorm.io/datatypes"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/rate"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
)

var (
	ErrTenantNotFound = errors.New("tenant not found")
	ErrInvalidRequest = errors.New("invalid request")
)

//go:embed rego/decision.rego
var policyModule string

// Repository captures the storage needs for policy operations.
type Repository interface {
	CreateTenant(ctx context.Context, tenant *gormdb.Tenant) error
	EnsureTenantExists(ctx context.Context, tenantID uuid.UUID) error
	TenantByID(ctx context.Context, tenantID uuid.UUID) (*gormdb.Tenant, error)
	ListPolicyRules(ctx context.Context, tenantID uuid.UUID) ([]gormdb.PolicyRule, error)
	ReplacePolicyRules(ctx context.Context, tenantID uuid.UUID, rules []*gormdb.PolicyRule) error
	UpsertGroup(ctx context.Context, group *gormdb.Group) error
	GroupsByTenant(ctx context.Context, tenantID uuid.UUID) ([]gormdb.Group, error)
	RecordQuotaSnapshot(ctx context.Context, snapshot *gormdb.QuotaSnapshot) error
	UpdateUserState(ctx context.Context, userID uuid.UUID, state string) error
	UserByID(ctx context.Context, userID uuid.UUID) (*gormdb.User, error)
}

// ServiceConfig tunes policy behaviour.
type ServiceConfig struct {
	QuotaWindow time.Duration
}

// Service coordinates RBAC evaluation and quota checks.
type Service struct {
	repo      Repository
	bucket    rate.TokenBucket
	publisher events.PolicyPublisher
	logger    *slog.Logger
	cfg       ServiceConfig
	decisionQ rego.PreparedEvalQuery
}

// RuleSpec describes an admin-managed policy rule.
type RuleSpec struct {
	Name       string         `json:"name"`
	Effect     string         `json:"effect"`
	Actions    []string       `json:"actions"`
	Resources  []string       `json:"resources"`
	Conditions map[string]any `json:"conditions"`
}

// GroupRequest captures group creation/update parameters.
type GroupRequest struct {
	TenantID        uuid.UUID
	Name            string
	Roles           []string
	SpeechQuota     int64
	LLMTokensQuota  int64
	ScreenshotQuota int64
	UpdatedBy       uuid.UUID
	OverdraftPolicy map[string]any
}

// TenantRequest defines tenant creation parameters.
type TenantRequest struct {
	Name string
	Tier string
}

// CheckAccessRequest describes an RBAC evaluation.
type CheckAccessRequest struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
	Roles    []string
	Action   string
	Resource string
}

// AccessDecision captures the result of RBAC evaluation.
type AccessDecision struct {
	Allowed  bool
	Reason   string
	RuleName string
}

// QuotaRequest captures quota consumption intent.
type QuotaRequest struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
	Roles    []string
	Resource string
	Amount   int64
}

// QuotaDecision represents the bucket evaluation outcome.
type QuotaDecision struct {
	Allowed   bool
	Reason    string
	State     string
	Remaining float64
}

// NewService constructs the policy service instance.
func NewService(repo Repository, bucket rate.TokenBucket, publisher events.PolicyPublisher, logger *slog.Logger, cfg ServiceConfig) (*Service, error) {
	if repo == nil {
		return nil, errors.New("policy repository required")
	}
	if cfg.QuotaWindow <= 0 {
		cfg.QuotaWindow = 24 * time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	prepared, err := rego.New(
		rego.Query("data.cloudaccess.decision"),
		rego.Module("cloudaccess_policy.rego", policyModule),
	).PrepareForEval(context.Background())
	if err != nil {
		return nil, fmt.Errorf("compile policy rego: %w", err)
	}
	return &Service{
		repo:      repo,
		bucket:    bucket,
		publisher: publisher,
		logger:    logger.With("component", "policy_service"),
		cfg:       cfg,
		decisionQ: prepared,
	}, nil
}

// CreateTenant persists a new tenant record.
func (s *Service) CreateTenant(ctx context.Context, req TenantRequest) (*gormdb.Tenant, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrInvalidRequest
	}
	tenant := &gormdb.Tenant{
		Name: name,
		Tier: sanitizeTier(req.Tier),
	}
	if err := s.repo.CreateTenant(ctx, tenant); err != nil {
		return nil, err
	}
	return tenant, nil
}

// SavePolicyRules replaces the tenant's ruleset.
func (s *Service) SavePolicyRules(ctx context.Context, tenantID uuid.UUID, rules []RuleSpec) ([]gormdb.PolicyRule, error) {
	if tenantID == uuid.Nil {
		return nil, ErrInvalidRequest
	}
	if err := s.repo.EnsureTenantExists(ctx, tenantID); err != nil {
		if errors.Is(err, gormdb.ErrNotFound) {
			return nil, ErrTenantNotFound
		}
		return nil, err
	}
	dbRules := make([]*gormdb.PolicyRule, 0, len(rules))
	for _, rule := range rules {
		if strings.TrimSpace(rule.Name) == "" {
			continue
		}
		effect := strings.ToLower(strings.TrimSpace(rule.Effect))
		if effect == "" {
			effect = "allow"
		}
		dbRules = append(dbRules, &gormdb.PolicyRule{
			TenantID:   tenantID,
			Name:       rule.Name,
			Effect:     effect,
			Actions:    pq.StringArray(copyStrings(rule.Actions)),
			Resources:  pq.StringArray(copyStrings(rule.Resources)),
			Conditions: datatypes.JSONMap(copyMap(rule.Conditions)),
		})
	}
	if err := s.repo.ReplacePolicyRules(ctx, tenantID, dbRules); err != nil {
		return nil, err
	}
	if s.publisher != nil {
		_ = s.publisher.PublishPolicyUpdate(ctx, tenantID, map[string]any{"rules": len(dbRules)})
	}
	return s.repo.ListPolicyRules(ctx, tenantID)
}

// ListPolicyRules fetches tenant rules.
func (s *Service) ListPolicyRules(ctx context.Context, tenantID uuid.UUID) ([]gormdb.PolicyRule, error) {
	if tenantID == uuid.Nil {
		return nil, ErrInvalidRequest
	}
	if err := s.repo.EnsureTenantExists(ctx, tenantID); err != nil {
		if errors.Is(err, gormdb.ErrNotFound) {
			return nil, ErrTenantNotFound
		}
		return nil, err
	}
	rules, err := s.repo.ListPolicyRules(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return rules, nil
}

// UpsertGroup creates or updates a group with quotas.
func (s *Service) UpsertGroup(ctx context.Context, req GroupRequest) (*gormdb.Group, error) {
	if req.TenantID == uuid.Nil || strings.TrimSpace(req.Name) == "" {
		return nil, ErrInvalidRequest
	}
	group := &gormdb.Group{
		TenantID:           req.TenantID,
		Name:               strings.TrimSpace(req.Name),
		Roles:              pq.StringArray(copyStrings(req.Roles)),
		SpeechQuotaSeconds: req.SpeechQuota,
		LlmTokenQuota:      req.LLMTokensQuota,
		ScreenshotQuota:    req.ScreenshotQuota,
		OverdraftPolicy:    datatypes.JSONMap(copyMap(req.OverdraftPolicy)),
		UpdatedBy:          req.UpdatedBy,
	}
	if err := s.repo.UpsertGroup(ctx, group); err != nil {
		return nil, err
	}
	if s.publisher != nil {
		_ = s.publisher.PublishPolicyUpdate(ctx, req.TenantID, map[string]any{"group": group.Name})
	}
	return group, nil
}

// CheckAccess evaluates RBAC policies for the request.
func (s *Service) CheckAccess(ctx context.Context, req CheckAccessRequest) (*AccessDecision, error) {
	if req.TenantID == uuid.Nil || strings.TrimSpace(req.Action) == "" {
		return nil, ErrInvalidRequest
	}
	rules, err := s.repo.ListPolicyRules(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}
	input := map[string]any{
		"roles":    lowerSlice(req.Roles),
		"action":   strings.ToLower(req.Action),
		"resource": strings.ToLower(req.Resource),
		"rules":    prepareRules(rules),
	}
	results, err := s.decisionQ.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return nil, fmt.Errorf("eval policy: %w", err)
	}
	decision := AccessDecision{}
	if len(results) == 0 || len(results[0].Expressions) == 0 {
		return &decision, nil
	}
	payload, ok := results[0].Expressions[0].Value.(map[string]any)
	if !ok {
		return &decision, nil
	}
	if allowed, ok := payload["allow"].(bool); ok {
		decision.Allowed = allowed
	}
	if reason, ok := payload["reason"].(string); ok {
		decision.Reason = reason
	}
	if rule, ok := payload["rule_name"].(string); ok {
		decision.RuleName = rule
	}
	return &decision, nil
}

// EvaluateQuota enforces quota constraints using the configured bucket.
func (s *Service) EvaluateQuota(ctx context.Context, req QuotaRequest) (*QuotaDecision, error) {
	if req.UserID == uuid.Nil {
		return nil, ErrInvalidRequest
	}
	user, err := s.repo.UserByID(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	tenantID := req.TenantID
	if tenantID == uuid.Nil {
		tenantID = user.TenantID
	}
	if tenantID != user.TenantID {
		return nil, ErrInvalidRequest
	}
	roles := req.Roles
	if len(roles) == 0 {
		roles = copyStrings([]string(user.Roles))
	}
	resource := normalizeResource(req.Resource)
	if resource == "" {
		resource = "speech"
	}
	groups, err := s.repo.GroupsByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	capacity := quotaCapacity(groups, roles, resource)
	decision := &QuotaDecision{Allowed: true, Reason: "quota_ok", State: "active", Remaining: capacity}
	amount := float64(req.Amount)
	if capacity <= 0 || amount <= 0 {
		return decision, nil
	}
	refillRate := capacity / s.cfg.QuotaWindow.Seconds()
	key := fmt.Sprintf("%s:%s:%s", tenantID.String(), user.ID.String(), resource)
	var bucketResult rate.Result
	var bucketErr error
	if s.bucket == nil {
		bucketResult = rate.Result{Allowed: true, Remaining: capacity}
	} else {
		bucketResult, bucketErr = s.bucket.Take(ctx, key, capacity, refillRate, amount, s.cfg.QuotaWindow)
	}
	if bucketErr != nil {
		return nil, bucketErr
	}
	decision.Allowed = bucketResult.Allowed
	decision.Remaining = bucketResult.Remaining
	decision.State = deriveState(capacity, bucketResult.Remaining, bucketResult.Allowed)
	if !bucketResult.Allowed {
		decision.Reason = "quota_exceeded"
	} else if decision.State == "warning" {
		decision.Reason = "quota_warning"
	}
	s.persistQuotaSnapshot(ctx, tenantID, user.ID, resource, capacity-bucketResult.Remaining, decision.State)
	if err := s.maybeUpdateUserState(ctx, user, decision.State); err != nil {
		s.logger.Warn("failed to update user state", "error", err)
	}
	return decision, nil
}

func (s *Service) persistQuotaSnapshot(ctx context.Context, tenantID, userID uuid.UUID, resource string, used float64, state string) {
	snapshot := &gormdb.QuotaSnapshot{
		TenantID:   tenantID,
		UserID:     userID,
		State:      state,
		CapturedAt: time.Now().UTC(),
	}
	usedInt := int64(math.Round(math.Max(used, 0)))
	switch resource {
	case "speech":
		snapshot.SpeechSecondsUsed = usedInt
	case "llm":
		snapshot.LLMTokensUsed = usedInt
	case "screenshot":
		snapshot.ScreenshotsUsed = usedInt
	default:
		return
	}
	if err := s.repo.RecordQuotaSnapshot(ctx, snapshot); err != nil {
		s.logger.Warn("failed to record quota snapshot", "error", err)
	}
}

func (s *Service) maybeUpdateUserState(ctx context.Context, user *gormdb.User, nextState string) error {
	if user == nil {
		return nil
	}
	if nextState == "" || strings.EqualFold(user.State, nextState) {
		return nil
	}
	if err := s.repo.UpdateUserState(ctx, user.ID, nextState); err != nil {
		return err
	}
	user.State = nextState
	return nil
}

func sanitizeTier(tier string) string {
	tier = strings.ToLower(strings.TrimSpace(tier))
	if tier == "" {
		tier = "standard"
	}
	return tier
}

func lowerSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, strings.ToLower(strings.TrimSpace(v)))
	}
	return out
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func copyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func copyJSONMap(src datatypes.JSONMap) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func prepareRules(rules []gormdb.PolicyRule) []map[string]any {
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		out = append(out, map[string]any{
			"name":       rule.Name,
			"effect":     strings.ToLower(rule.Effect),
			"actions":    []string(rule.Actions),
			"resources":  []string(rule.Resources),
			"conditions": copyJSONMap(rule.Conditions),
		})
	}
	return out
}

func normalizeResource(res string) string {
	res = strings.ToLower(strings.TrimSpace(res))
	switch res {
	case "speech", "speech_seconds", "audio":
		return "speech"
	case "llm", "llm_tokens", "tokens":
		return "llm"
	case "screenshot", "screenshots":
		return "screenshot"
	default:
		return res
	}
}

func quotaCapacity(groups []gormdb.Group, roles []string, resource string) float64 {
	roleSet := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		roleSet[strings.ToLower(strings.TrimSpace(role))] = struct{}{}
	}
	var total int64
	for _, group := range groups {
		if !groupMatchesRoles(group, roleSet) {
			continue
		}
		switch resource {
		case "llm":
			total += group.LlmTokenQuota
		case "screenshot":
			total += group.ScreenshotQuota
		default:
			total += group.SpeechQuotaSeconds
		}
	}
	if total <= 0 {
		return 0
	}
	return float64(total)
}

func groupMatchesRoles(group gormdb.Group, roles map[string]struct{}) bool {
	if len(group.Roles) == 0 {
		return true
	}
	if len(roles) == 0 {
		return false
	}
	for _, role := range group.Roles {
		if _, ok := roles[strings.ToLower(role)]; ok {
			return true
		}
	}
	return false
}

func deriveState(capacity, remaining float64, allowed bool) string {
	if capacity <= 0 {
		return "active"
	}
	used := capacity - remaining
	if used < 0 {
		used = 0
	}
	ratio := used / capacity
	state := "active"
	if ratio >= 0.9 {
		state = "warning"
	}
	if !allowed {
		state = "frozen"
	}
	return state
}
