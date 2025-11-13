package gateway

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/policy"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/rate"
)

// TokenValidator encapsulates the IAM validation dependency.
type TokenValidator interface {
	Validate(ctx context.Context, token string, kind crypto.TokenKind) (*crypto.Claims, error)
}

// PolicyEvaluator captures the policy dependency.
type PolicyEvaluator interface {
	CheckAccess(ctx context.Context, req policy.CheckAccessRequest) (*policy.AccessDecision, error)
}

// Server implements the API gateway HTTP handler.
type Server struct {
	validator TokenValidator
	policy    PolicyEvaluator
	bucket    rate.TokenBucket
	logger    *slog.Logger
	cfg       Config
	routes    atomic.Pointer[compiledRouter]
}

// compiledRouter holds the hot-swappable chi router and metadata.
type compiledRouter struct {
	handler http.Handler
	set     *RouteSet
}

// NewServer constructs a gateway server.
func NewServer(validator TokenValidator, policySvc PolicyEvaluator, bucket rate.TokenBucket, logger *slog.Logger, cfg Config) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{
		validator: validator,
		policy:    policySvc,
		bucket:    bucket,
		logger:    logger.With("component", "gateway"),
		cfg:       cfg,
	}
	return server
}

// Enabled returns whether the gateway should be wired.
func (s *Server) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

// Register attaches the gateway handlers onto the provided mux.
func (s *Server) Register(mux *http.ServeMux) {
	if mux == nil || !s.Enabled() {
		return
	}
	mux.Handle("/api/", s)
	mux.Handle("/ws/", s)
	mux.Handle("/_admin/gateway/routes", http.HandlerFunc(s.handleRoutesPush))
	mux.Handle("/_admin/gateway/certs", http.HandlerFunc(s.handleCertRotate))
}

// ServeHTTP satisfies http.Handler for routing requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	router := s.routes.Load()
	if router == nil || router.handler == nil {
		writeGatewayError(w, r, http.StatusServiceUnavailable, "50301", "gateway routes not loaded")
		return
	}
	router.handler.ServeHTTP(w, r)
}

// LoadRoutes loads the configured route file if present.
func (s *Server) LoadRoutes() error {
	if strings.TrimSpace(s.cfg.RoutesPath) == "" {
		return nil
	}
	set, err := LoadRouteSetFromFile(s.cfg.RoutesPath)
	if err != nil {
		return err
	}
	return s.ApplyRouteSet(set)
}

// ApplyRouteSet compiles routes and swaps the active router.
func (s *Server) ApplyRouteSet(set *RouteSet) error {
	if set == nil {
		return errors.New("route set missing")
	}
	cloned, err := CloneRouteSet(set)
	if err != nil {
		return err
	}
	router, err := s.buildRouter(cloned)
	if err != nil {
		return err
	}
	s.routes.Store(router)
	s.logger.Info("gateway routes applied", "routes", len(cloned.Routes), "version", cloned.Version)
	return nil
}

// buildRouter compiles chi routes for the provided definition.
func (s *Server) buildRouter(set *RouteSet) (*compiledRouter, error) {
	r := chi.NewRouter()
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		writeGatewayError(w, req, http.StatusNotFound, "40400", "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		writeGatewayError(w, req, http.StatusMethodNotAllowed, "40500", "method not allowed")
	})
	for _, raw := range set.Routes {
		if strings.TrimSpace(raw.Path) == "" || len(raw.Methods) == 0 {
			continue
		}
		route := raw // capture copy
		handler, err := s.buildHandler(route)
		if err != nil {
			return nil, fmt.Errorf("route %s: %w", route.Name, err)
		}
		for _, method := range route.Methods {
			m := strings.ToUpper(strings.TrimSpace(method))
			if m == "" {
				continue
			}
			r.Method(m, route.Path, handler)
		}
	}
	return &compiledRouter{handler: r, set: set}, nil
}

// buildHandler composes the middleware stack for a single route.
func (s *Server) buildHandler(route RouteDefinition) (http.HandlerFunc, error) {
	proxy, err := s.newProxy(route)
	if err != nil {
		return nil, err
	}
	requiredRoles := make([]string, 0, len(route.Auth.RequiredRoles))
	for _, role := range route.Auth.RequiredRoles {
		if trimmed := strings.TrimSpace(strings.ToLower(role)); trimmed != "" {
			requiredRoles = append(requiredRoles, trimmed)
		}
	}
	action := strings.TrimSpace(strings.ToLower(route.Auth.Action))
	resource := strings.TrimSpace(strings.ToLower(route.Auth.Resource))

	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var claims *crypto.Claims
		var err error
		if !route.Auth.AllowAnonymous {
			token, verr := extractBearerToken(r)
			if verr != nil {
				writeGatewayError(w, r, http.StatusUnauthorized, "40101", verr.Error())
				return
			}
			if s.validator == nil {
				writeGatewayError(w, r, http.StatusInternalServerError, "50001", "token validator unavailable")
				return
			}
			kind := tokenKind(route.Auth.TokenFormat)
			claims, err = s.validator.Validate(ctx, token, kind)
			if err != nil {
				writeGatewayError(w, r, http.StatusUnauthorized, "40101", "invalid token")
				return
			}
			ctx = withClaims(ctx, claims)
		}
		if claims != nil && len(requiredRoles) > 0 && !hasRole(claims.Roles, requiredRoles) {
			writeGatewayError(w, r, http.StatusForbidden, "40301", "insufficient role")
			return
		}
		if claims != nil && action != "" {
			if s.policy == nil {
				writeGatewayError(w, r, http.StatusInternalServerError, "50002", "policy evaluator unavailable")
				return
			}
			tenantID, userID, parseErr := parseClaimsIDs(claims)
			if parseErr != nil {
				writeGatewayError(w, r, http.StatusUnauthorized, "40101", "invalid claims")
				return
			}
			decision, authErr := s.policy.CheckAccess(ctx, policy.CheckAccessRequest{
				TenantID: tenantID,
				UserID:   userID,
				Roles:    claims.Roles,
				Action:   action,
				Resource: resource,
			})
			if authErr != nil {
				s.logger.Error("policy check failed", "error", authErr, "route", route.Name)
				writeGatewayError(w, r, http.StatusForbidden, "40302", "policy evaluation failed")
				return
			}
			if decision == nil || !decision.Allowed {
				writeGatewayError(w, r, http.StatusForbidden, "40301", "policy denied")
				return
			}
		}
		if err := s.takeRateLimit(ctx, route, claims); err != nil {
			var rl *rateLimitError
			if errors.As(err, &rl) {
				headers := w.Header()
				if rl.RetryAfter > 0 {
					headers.Set("Retry-After", fmt.Sprintf("%.0f", rl.RetryAfter.Seconds()))
				}
				writeGatewayError(w, r, http.StatusTooManyRequests, "42901", "rate limit exceeded")
				return
			}
			s.logger.Warn("rate limiter error", "error", err, "route", route.Name)
			writeGatewayError(w, r, http.StatusTooManyRequests, "42901", "rate limit error")
			return
		}

		req := r.Clone(ctx)
		if claims != nil {
			req.Header.Set("X-CloudAccess-Tenant", claims.TenantID)
			req.Header.Set("X-CloudAccess-User", claims.Subject)
			if len(claims.Roles) > 0 {
				req.Header.Set("X-CloudAccess-Roles", strings.Join(claims.Roles, ","))
			}
			req.Header.Set("X-CloudAccess-Session", claims.SessionID)
		}
		for key, value := range route.Headers {
			if value == "" {
				continue
			}
			req.Header.Set(key, value)
		}
		proxy.ServeHTTP(w, req)
	}, nil
}

func (s *Server) newProxy(route RouteDefinition) (*httputil.ReverseProxy, error) {
	targetURL, err := url.Parse(strings.TrimSpace(route.Upstream.URL))
	if err != nil {
		return nil, fmt.Errorf("parse upstream url: %w", err)
	}
	if targetURL.Scheme == "" {
		targetURL.Scheme = "http"
	}
	timeout := time.Duration(route.Upstream.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSHandshakeTimeout:   10 * time.Second,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: timeout,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: route.Upstream.InsecureSkipVerify, //nolint:gosec
		},
	}
	stripPrefix := route.StripPathPrefix
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = transport
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		s.logger.Warn("proxy error", "error", err, "route", route.Name)
		writeGatewayError(w, r, http.StatusBadGateway, "50200", "upstream unavailable")
	}
	proxy.FlushInterval = 50 * time.Millisecond
	proxy.Director = func(req *http.Request) {
		requestURL := *targetURL
		req.URL.Scheme = requestURL.Scheme
		req.URL.Host = requestURL.Host
		req.Host = requestURL.Host

		path := req.URL.Path
		if stripPrefix != "" && strings.HasPrefix(path, stripPrefix) {
			path = strings.TrimPrefix(path, stripPrefix)
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
		}
		req.URL.Path = singleJoiningSlash(requestURL.Path, path)
		if requestURL.RawQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = requestURL.RawQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = requestURL.RawQuery + "&" + req.URL.RawQuery
		}
		if _, ok := req.Header["User-Agent"]; !ok {
			req.Header.Set("User-Agent", "")
		}
	}
	return proxy, nil
}

// takeRateLimit applies the configured rate limit, if any.
func (s *Server) takeRateLimit(ctx context.Context, route RouteDefinition, claims *crypto.Claims) error {
	if s.bucket == nil {
		return nil
	}
	cfg := route.RateLimit
	capacity := cfg.Capacity
	if capacity <= 0 {
		capacity = s.cfg.DefaultCapacity
	}
	if capacity <= 0 {
		return nil
	}
	refill := cfg.RefillPerSecond
	if refill <= 0 {
		if s.cfg.DefaultRefillPerSec > 0 {
			refill = s.cfg.DefaultRefillPerSec
		} else {
			refill = capacity
		}
	}
	window := time.Duration(cfg.WindowSeconds) * time.Second
	if window <= 0 {
		if s.cfg.DefaultWindow > 0 {
			window = s.cfg.DefaultWindow
		} else {
			window = time.Minute
		}
	}
	amount := cfg.Amount
	if amount <= 0 {
		amount = 1
	}
	key := fmt.Sprintf("route:%s", sanitizeKey(route.Name))
	if cfg.PerTenant && claims != nil {
		key = fmt.Sprintf("%s:tenant:%s", key, claims.TenantID)
	}
	if cfg.PerUser && claims != nil {
		key = fmt.Sprintf("%s:user:%s", key, claims.Subject)
	}
	res, err := s.bucket.Take(ctx, key, capacity, refill, amount, window)
	if err != nil {
		return err
	}
	if !res.Allowed {
		return &rateLimitError{RetryAfter: res.RetryAfter}
	}
	return nil
}

func sanitizeKey(name string) string {
	if name == "" {
		return "default"
	}
	return strings.NewReplacer(" ", "_", "/", "_").Replace(strings.ToLower(name))
}

func (s *Server) currentRouteSet() *RouteSet {
	router := s.routes.Load()
	if router == nil {
		return nil
	}
	return router.set
}

// handleRoutesPush handles admin route updates pushed by gatewayctl.
func (s *Server) handleRoutesPush(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		writeGatewayError(w, r, http.StatusNotFound, "40400", "gateway disabled")
		return
	}
	if !s.authorizeAdmin(r) {
		writeGatewayError(w, r, http.StatusUnauthorized, "40199", "invalid admin token")
		return
	}
	if r.Method != http.MethodPost {
		writeGatewayError(w, r, http.StatusMethodNotAllowed, "40500", "method not allowed")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, "40000", "invalid payload")
		return
	}
	defer r.Body.Close()
	var set RouteSet
	if err := json.Unmarshal(body, &set); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, "40000", "invalid routes document")
		return
	}
	if len(set.Routes) == 0 {
		writeGatewayError(w, r, http.StatusBadRequest, "40000", "no routes provided")
		return
	}
	if err := s.ApplyRouteSet(&set); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, "40000", err.Error())
		return
	}
	if err := s.persistRoutes(&set); err != nil {
		s.logger.Warn("failed to persist routes", "error", err)
	}
	_ = writeJSON(w, map[string]any{"status": "reloaded", "routes": len(set.Routes), "version": set.Version})
}

// handleCertRotate accepts certificate payloads for future mTLS wiring.
func (s *Server) handleCertRotate(w http.ResponseWriter, r *http.Request) {
	if !s.Enabled() {
		writeGatewayError(w, r, http.StatusNotFound, "40400", "gateway disabled")
		return
	}
	if !s.authorizeAdmin(r) {
		writeGatewayError(w, r, http.StatusUnauthorized, "40199", "invalid admin token")
		return
	}
	if r.Method != http.MethodPost {
		writeGatewayError(w, r, http.StatusMethodNotAllowed, "40500", "method not allowed")
		return
	}
	var payload struct {
		Certificate string `json:"certificate"`
		PrivateKey  string `json:"private_key"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&payload); err != nil {
		writeGatewayError(w, r, http.StatusBadRequest, "40000", "invalid payload")
		return
	}
	defer r.Body.Close()
	if strings.TrimSpace(payload.Certificate) == "" || strings.TrimSpace(payload.PrivateKey) == "" {
		writeGatewayError(w, r, http.StatusBadRequest, "40000", "certificate and key required")
		return
	}
	if err := s.persistCertificate(payload.Certificate, payload.PrivateKey); err != nil {
		s.logger.Warn("failed to persist certificate", "error", err)
		writeGatewayError(w, r, http.StatusInternalServerError, "50003", "persist certificate failed")
		return
	}
	_ = writeJSON(w, map[string]any{"status": "stored"})
}

func (s *Server) persistRoutes(set *RouteSet) error {
	if strings.TrimSpace(s.cfg.RoutesPath) == "" {
		return nil
	}
	data, err := MarshalRouteSet(set)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.cfg.RoutesPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.cfg.RoutesPath, data, 0o600)
}

func (s *Server) persistCertificate(cert, key string) error {
	if strings.TrimSpace(s.cfg.CertBundlePath) == "" {
		return nil
	}
	data := fmt.Sprintf("%s\n---\n%s\n", strings.TrimSpace(cert), strings.TrimSpace(key))
	if err := os.MkdirAll(filepath.Dir(s.cfg.CertBundlePath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.cfg.CertBundlePath, []byte(data), 0o600)
}

func (s *Server) authorizeAdmin(r *http.Request) bool {
	expected := strings.TrimSpace(s.cfg.AdminToken)
	if expected == "" {
		return true
	}
	token := strings.TrimSpace(r.Header.Get("X-Admin-Token"))
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if len(token) == 0 {
		return false
	}
	if len(token) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(expected)) == 1
}

func parseClaimsIDs(claims *crypto.Claims) (uuid.UUID, uuid.UUID, error) {
	if claims == nil {
		return uuid.Nil, uuid.Nil, errors.New("claims missing")
	}
	tenantID, err := uuid.Parse(claims.TenantID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return tenantID, userID, nil
}

// writeJSON is a lightweight helper to respond with JSON.
func writeJSON(w http.ResponseWriter, payload any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(payload)
}
