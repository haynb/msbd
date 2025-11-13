package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/policy"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/rate"
)

func TestGatewayRequiresAuth(t *testing.T) {
	upstreamHit := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHit <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)

	server := NewServer(
		&fakeValidator{claims: stubClaims()},
		&allowPolicy{},
		&fakeBucket{result: rate.Result{Allowed: true}},
		nil,
		Config{Enabled: true, DefaultCapacity: 10, DefaultRefillPerSec: 5, DefaultWindow: time.Second},
	)
	err := server.ApplyRouteSet(&RouteSet{
		Version: "v1",
		Routes: []RouteDefinition{
			{
				Name:            "secure",
				Methods:         []string{"GET"},
				Path:            "/api/secure",
				StripPathPrefix: "/api",
				Upstream:        UpstreamTarget{URL: upstream.URL},
				Auth:            RouteAuthConfig{TokenFormat: "jwt"},
			},
		},
	})
	if err != nil {
		t.Fatalf("apply routes: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/secure", nil)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/secure", nil)
	req2.Header.Set("Authorization", "Bearer token")
	rr2 := httptest.NewRecorder()
	server.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr2.Code)
	}
	select {
	case <-upstreamHit:
	case <-time.After(time.Second):
		t.Fatal("upstream not hit")
	}
}

func TestGatewayRateLimit(t *testing.T) {
	server := NewServer(
		&fakeValidator{claims: stubClaims()},
		&allowPolicy{},
		&fakeBucket{result: rate.Result{Allowed: false, RetryAfter: time.Second}},
		nil,
		Config{Enabled: true, DefaultCapacity: 1, DefaultRefillPerSec: 1, DefaultWindow: time.Second},
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be hit")
	}))
	t.Cleanup(upstream.Close)
	if err := server.ApplyRouteSet(&RouteSet{
		Version: "v1",
		Routes: []RouteDefinition{
			{
				Name:     "limited",
				Methods:  []string{"GET"},
				Path:     "/api/limited",
				Upstream: UpstreamTarget{URL: upstream.URL},
			},
		},
	}); err != nil {
		t.Fatalf("apply routes: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/limited", nil)
	req.Header.Set("Authorization", "Bearer token")
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header")
	}
}

func TestGatewayAdminPush(t *testing.T) {
	tmp := t.TempDir()
	routesPath := filepath.Join(tmp, "routes.yaml")
	server := NewServer(
		&fakeValidator{claims: stubClaims()},
		&allowPolicy{},
		&fakeBucket{result: rate.Result{Allowed: true}},
		nil,
		Config{
			Enabled:             true,
			AdminToken:          "secret",
			RoutesPath:          routesPath,
			DefaultCapacity:     10,
			DefaultRefillPerSec: 5,
			DefaultWindow:       time.Second,
		},
	)
	mux := http.NewServeMux()
	server.Register(mux)

	api := httptest.NewServer(mux)
	t.Cleanup(api.Close)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(upstream.Close)

	set := RouteSet{
		Version: "v1",
		Routes: []RouteDefinition{
			{
				Name:    "admin",
				Methods: []string{"GET"},
				Path:    "/api/admin",
				Upstream: UpstreamTarget{
					URL: upstream.URL,
				},
				Auth: RouteAuthConfig{AllowAnonymous: true},
			},
		},
	}
	body, _ := json.Marshal(set)
	req, _ := http.NewRequest(http.MethodPost, api.URL+"/_admin/gateway/routes", bytes.NewReader(body))
	req.Header.Set("X-Admin-Token", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("push routes: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	content, err := os.ReadFile(routesPath)
	if err != nil {
		t.Fatalf("read routes: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("routes file empty")
	}

	clientResp, err := http.Get(api.URL + "/api/admin")
	if err != nil {
		t.Fatalf("gateway request: %v", err)
	}
	clientResp.Body.Close()
	if clientResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", clientResp.StatusCode)
	}
}

type fakeValidator struct {
	claims *crypto.Claims
	err    error
}

func (f *fakeValidator) Validate(ctx context.Context, token string, kind crypto.TokenKind) (*crypto.Claims, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.claims, nil
}

type allowPolicy struct{}

func (allowPolicy) CheckAccess(ctx context.Context, req policy.CheckAccessRequest) (*policy.AccessDecision, error) {
	return &policy.AccessDecision{Allowed: true}, nil
}

type fakeBucket struct {
	result  rate.Result
	err     error
	lastKey string
}

func (f *fakeBucket) Take(ctx context.Context, key string, capacity, refill, amount float64, ttl time.Duration) (rate.Result, error) {
	f.lastKey = key
	if f.err != nil {
		return rate.Result{}, f.err
	}
	return f.result, nil
}

func stubClaims() *crypto.Claims {
	return &crypto.Claims{
		Subject:   "11111111-1111-1111-1111-111111111111",
		TenantID:  "22222222-2222-2222-2222-222222222222",
		SessionID: "sess",
		Roles:     []string{"admin"},
	}
}
