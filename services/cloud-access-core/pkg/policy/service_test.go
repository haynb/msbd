package policy

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/rate"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
)

func TestCheckAccessAllowsRule(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	tenant := repo.seedTenant("acme")
	repo.rules[tenant.ID] = []gormdb.PolicyRule{{TenantID: tenant.ID, Name: "allow_read", Effect: "allow", Actions: pq.StringArray{"policy.read"}, Resources: pq.StringArray{"/foo"}}}

	svc := newTestService(t, repo, &fakeBucket{})
	decision, err := svc.CheckAccess(ctx, CheckAccessRequest{TenantID: tenant.ID, Roles: []string{"admin"}, Action: "policy.read", Resource: "/foo"})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
}

func TestCheckAccessDeniesExplicitRule(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	tenant := repo.seedTenant("acme")
	repo.rules[tenant.ID] = []gormdb.PolicyRule{{TenantID: tenant.ID, Name: "deny_all", Effect: "deny", Actions: pq.StringArray{"*"}, Resources: pq.StringArray{"*"}}}

	svc := newTestService(t, repo, &fakeBucket{})
	decision, err := svc.CheckAccess(ctx, CheckAccessRequest{TenantID: tenant.ID, Roles: []string{"basic"}, Action: "policy.write", Resource: "/"})
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, "deny_all", decision.RuleName)
}

func TestEvaluateQuotaUsesBucketAndUpdatesState(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	tenant := repo.seedTenant("acme")
	user := repo.seedUser(tenant.ID, []string{"basic"})
	repo.groups[tenant.ID] = []gormdb.Group{{TenantID: tenant.ID, Name: "basic", Roles: pq.StringArray{"basic"}, SpeechQuotaSeconds: 100}}

	bucket := &fakeBucket{}
	svc := newTestService(t, repo, bucket)

	decision, err := svc.EvaluateQuota(ctx, QuotaRequest{TenantID: tenant.ID, UserID: user.ID, Roles: []string{"basic"}, Resource: "speech", Amount: 60})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.Equal(t, "active", decision.State)

	bucket.deny = true
	decision, err = svc.EvaluateQuota(ctx, QuotaRequest{TenantID: tenant.ID, UserID: user.ID, Roles: []string{"basic"}, Resource: "speech", Amount: 50})
	require.NoError(t, err)
	require.False(t, decision.Allowed)
	require.Equal(t, "frozen", decision.State)
	require.Equal(t, "frozen", repo.users[user.ID].State)
	require.NotEmpty(t, repo.snapshots)
}

func newTestService(t *testing.T, repo *memoryRepo, bucket rate.TokenBucket) *Service {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc, err := NewService(repo, bucket, nil, logger, ServiceConfig{QuotaWindow: time.Hour})
	require.NoError(t, err)
	return svc
}

type fakeBucket struct {
	deny      bool
	remaining map[string]float64
}

func (f *fakeBucket) Take(_ context.Context, key string, capacity float64, _ float64, amount float64, _ time.Duration) (rate.Result, error) {
	if f.remaining == nil {
		f.remaining = make(map[string]float64)
	}
	remaining := f.remaining[key]
	if remaining == 0 {
		remaining = capacity
	}
	if f.deny || amount > remaining {
		f.remaining[key] = remaining
		return rate.Result{Allowed: false, Remaining: remaining}, nil
	}
	remaining -= amount
	f.remaining[key] = remaining
	return rate.Result{Allowed: true, Remaining: remaining}, nil
}

type memoryRepo struct {
	tenants   map[uuid.UUID]*gormdb.Tenant
	users     map[uuid.UUID]*gormdb.User
	rules     map[uuid.UUID][]gormdb.PolicyRule
	groups    map[uuid.UUID][]gormdb.Group
	snapshots []*gormdb.QuotaSnapshot
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{
		tenants: make(map[uuid.UUID]*gormdb.Tenant),
		users:   make(map[uuid.UUID]*gormdb.User),
		rules:   make(map[uuid.UUID][]gormdb.PolicyRule),
		groups:  make(map[uuid.UUID][]gormdb.Group),
	}
}

func (m *memoryRepo) seedTenant(name string) *gormdb.Tenant {
	tenant := &gormdb.Tenant{BaseModel: gormdb.BaseModel{ID: uuid.New()}, Name: name, Tier: "standard"}
	m.tenants[tenant.ID] = tenant
	return tenant
}

func (m *memoryRepo) seedUser(tenantID uuid.UUID, roles []string) *gormdb.User {
	user := &gormdb.User{BaseModel: gormdb.BaseModel{ID: uuid.New()}, TenantID: tenantID, Email: "user@example.com", Roles: pq.StringArray(roles), State: "active"}
	m.users[user.ID] = user
	return user
}

func (m *memoryRepo) CreateTenant(_ context.Context, tenant *gormdb.Tenant) error {
	if tenant.ID == uuid.Nil {
		tenant.ID = uuid.New()
	}
	m.tenants[tenant.ID] = tenant
	return nil
}

func (m *memoryRepo) EnsureTenantExists(_ context.Context, tenantID uuid.UUID) error {
	if _, ok := m.tenants[tenantID]; !ok {
		return gormdb.ErrNotFound
	}
	return nil
}

func (m *memoryRepo) TenantByID(_ context.Context, tenantID uuid.UUID) (*gormdb.Tenant, error) {
	tenant, ok := m.tenants[tenantID]
	if !ok {
		return nil, gormdb.ErrNotFound
	}
	copy := *tenant
	return &copy, nil
}

func (m *memoryRepo) ListPolicyRules(_ context.Context, tenantID uuid.UUID) ([]gormdb.PolicyRule, error) {
	rules := m.rules[tenantID]
	out := make([]gormdb.PolicyRule, len(rules))
	copy(out, rules)
	return out, nil
}

func (m *memoryRepo) ReplacePolicyRules(_ context.Context, tenantID uuid.UUID, rules []*gormdb.PolicyRule) error {
	copied := make([]gormdb.PolicyRule, len(rules))
	for i, rule := range rules {
		copied[i] = *rule
	}
	m.rules[tenantID] = copied
	return nil
}

func (m *memoryRepo) UpsertGroup(_ context.Context, group *gormdb.Group) error {
	groups := m.groups[group.TenantID]
	for i, existing := range groups {
		if existing.Name == group.Name {
			groups[i] = *group
			m.groups[group.TenantID] = groups
			return nil
		}
	}
	m.groups[group.TenantID] = append(groups, *group)
	return nil
}

func (m *memoryRepo) GroupsByTenant(_ context.Context, tenantID uuid.UUID) ([]gormdb.Group, error) {
	groups := m.groups[tenantID]
	out := make([]gormdb.Group, len(groups))
	copy(out, groups)
	return out, nil
}

func (m *memoryRepo) RecordQuotaSnapshot(_ context.Context, snapshot *gormdb.QuotaSnapshot) error {
	copy := *snapshot
	m.snapshots = append(m.snapshots, &copy)
	return nil
}

func (m *memoryRepo) UpdateUserState(_ context.Context, userID uuid.UUID, state string) error {
	if user, ok := m.users[userID]; ok {
		user.State = state
		return nil
	}
	return gormdb.ErrNotFound
}

func (m *memoryRepo) UserByID(_ context.Context, userID uuid.UUID) (*gormdb.User, error) {
	user, ok := m.users[userID]
	if !ok {
		return nil, gormdb.ErrNotFound
	}
	copy := *user
	return &copy, nil
}
