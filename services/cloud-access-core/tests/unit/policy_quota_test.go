package unit

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/policy"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/rate"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestPolicyService_CheckAccessAndQuota(t *testing.T) {
	ctx := context.Background()
	repo := newPolicyMemoryRepo()
	tenantID := uuid.New()
	userID := uuid.New()
	repo.tenants[tenantID] = &gormdb.Tenant{BaseModel: gormdb.BaseModel{ID: tenantID}, Name: "acme"}
	repo.users[userID] = &gormdb.User{
		BaseModel: gormdb.BaseModel{ID: userID},
		TenantID:  tenantID,
		Roles:     pq.StringArray{"admin"},
		State:     "active",
	}
	repo.groups[tenantID] = []gormdb.Group{
		{
			TenantID:           tenantID,
			Name:               "admins",
			Roles:              pq.StringArray{"admin"},
			SpeechQuotaSeconds: 600,
		},
	}
	repo.rules[tenantID] = []gormdb.PolicyRule{
		{
			TenantID:  tenantID,
			Name:      "allow-admin",
			Effect:    "allow",
			Actions:   pq.StringArray{"iam:login"},
			Resources: pq.StringArray{"*"},
		},
	}

	bucket := &stubBucket{result: rate.Result{Allowed: true, Remaining: 80}}
	svc, err := policy.NewService(repo, bucket, stubPolicyPublisher{}, slog.New(slog.NewTextHandler(io.Discard, nil)), policy.ServiceConfig{QuotaWindow: time.Hour})
	require.NoError(t, err)

	decision, err := svc.CheckAccess(ctx, policy.CheckAccessRequest{
		TenantID: tenantID,
		UserID:   userID,
		Roles:    []string{"admin"},
		Action:   "iam:login",
		Resource: "gateway",
	})
	require.NoError(t, err)
	require.True(t, decision.Allowed)

	quota, err := svc.EvaluateQuota(ctx, policy.QuotaRequest{
		TenantID: tenantID,
		UserID:   userID,
		Roles:    []string{"admin"},
		Resource: "speech",
		Amount:   300,
	})
	require.NoError(t, err)
	require.True(t, quota.Allowed)
	require.Zero(t, repo.stateUpdates[userID])

	bucket.result = rate.Result{Allowed: false, Remaining: 0}
	quota, err = svc.EvaluateQuota(ctx, policy.QuotaRequest{
		TenantID: tenantID,
		UserID:   userID,
		Roles:    []string{"admin"},
		Resource: "speech",
		Amount:   999,
	})
	require.NoError(t, err)
	require.False(t, quota.Allowed)
	require.Equal(t, "frozen", repo.stateUpdates[userID])
}

// --- test doubles ---

type policyMemoryRepo struct {
	tenants      map[uuid.UUID]*gormdb.Tenant
	users        map[uuid.UUID]*gormdb.User
	rules        map[uuid.UUID][]gormdb.PolicyRule
	groups       map[uuid.UUID][]gormdb.Group
	stateUpdates map[uuid.UUID]string
}

func newPolicyMemoryRepo() *policyMemoryRepo {
	return &policyMemoryRepo{
		tenants:      make(map[uuid.UUID]*gormdb.Tenant),
		users:        make(map[uuid.UUID]*gormdb.User),
		rules:        make(map[uuid.UUID][]gormdb.PolicyRule),
		groups:       make(map[uuid.UUID][]gormdb.Group),
		stateUpdates: make(map[uuid.UUID]string),
	}
}

func (m *policyMemoryRepo) CreateTenant(_ context.Context, tenant *gormdb.Tenant) error {
	m.tenants[tenant.ID] = tenant
	return nil
}

func (m *policyMemoryRepo) EnsureTenantExists(_ context.Context, tenantID uuid.UUID) error {
	if _, ok := m.tenants[tenantID]; !ok {
		return gormdb.ErrNotFound
	}
	return nil
}

func (m *policyMemoryRepo) TenantByID(_ context.Context, tenantID uuid.UUID) (*gormdb.Tenant, error) {
	if tenant, ok := m.tenants[tenantID]; ok {
		return tenant, nil
	}
	return nil, gormdb.ErrNotFound
}

func (m *policyMemoryRepo) ListPolicyRules(_ context.Context, tenantID uuid.UUID) ([]gormdb.PolicyRule, error) {
	return append([]gormdb.PolicyRule(nil), m.rules[tenantID]...), nil
}

func (m *policyMemoryRepo) ReplacePolicyRules(_ context.Context, tenantID uuid.UUID, rules []*gormdb.PolicyRule) error {
	m.rules[tenantID] = nil
	for _, r := range rules {
		cp := *r
		m.rules[tenantID] = append(m.rules[tenantID], cp)
	}
	return nil
}

func (m *policyMemoryRepo) UpsertGroup(_ context.Context, group *gormdb.Group) error {
	g := *group
	m.groups[group.TenantID] = append(m.groups[group.TenantID], g)
	return nil
}

func (m *policyMemoryRepo) GroupsByTenant(_ context.Context, tenantID uuid.UUID) ([]gormdb.Group, error) {
	return append([]gormdb.Group(nil), m.groups[tenantID]...), nil
}

func (m *policyMemoryRepo) RecordQuotaSnapshot(_ context.Context, _ *gormdb.QuotaSnapshot) error {
	return nil
}

func (m *policyMemoryRepo) UpdateUserState(_ context.Context, userID uuid.UUID, state string) error {
	m.stateUpdates[userID] = state
	return nil
}

func (m *policyMemoryRepo) UserByID(_ context.Context, userID uuid.UUID) (*gormdb.User, error) {
	if user, ok := m.users[userID]; ok {
		cp := *user
		return &cp, nil
	}
	return nil, gormdb.ErrNotFound
}

type stubBucket struct {
	result rate.Result
	err    error
}

func (s *stubBucket) Take(ctx context.Context, key string, capacity float64, refill float64, amount float64, ttl time.Duration) (rate.Result, error) {
	if s.err != nil {
		return rate.Result{}, s.err
	}
	return s.result, nil
}

type stubPolicyPublisher struct{}

func (stubPolicyPublisher) PublishPolicyUpdate(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
