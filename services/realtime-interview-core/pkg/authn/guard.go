package authn

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/iamproxy"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Guard enforces IAM authentication for HTTP and gRPC surfaces.
type Guard struct {
	validator iamproxy.Validator
	logger    *slog.Logger
}

// HTTPOptions configure the HTTP middleware.
type HTTPOptions struct {
	AllowAnonymous bool
	TokenFormat    crypto.TokenKind
	RequiredRoles  []string
}

// GRPCOptions configure the gRPC interceptors.
type GRPCOptions struct {
	AllowAnonymous bool
	TokenFormat    crypto.TokenKind
	RequiredRoles  []string
}

// NewGuard constructs a Guard.
func NewGuard(v iamproxy.Validator, logger *slog.Logger) *Guard {
	if logger == nil {
		logger = slog.Default()
	}
	return &Guard{validator: v, logger: logger.With("component", "authn.guard")}
}

// HTTPMiddleware returns an http.Handler middleware enforcing auth.
func (g *Guard) HTTPMiddleware(opts HTTPOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearer(r.Header.Get("Authorization"))
			if token == "" {
				if opts.AllowAnonymous {
					next.ServeHTTP(w, r)
					return
				}
				writeHTTPError(w, r, http.StatusUnauthorized, "missing authorization header")
				return
			}
			if g.validator == nil {
				writeHTTPError(w, r, http.StatusInternalServerError, "auth validator unavailable")
				return
			}
			claims, err := g.validator.Validate(r.Context(), token, resolveKind(opts.TokenFormat))
			if err != nil {
				g.logger.Warn("token validation failed", "error", err)
				writeHTTPError(w, r, http.StatusUnauthorized, "invalid token")
				return
			}
			if !hasRoles(claims.Roles, opts.RequiredRoles) {
				writeHTTPError(w, r, http.StatusForbidden, "insufficient role")
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UnaryInterceptor returns a unary gRPC interceptor enforcing IAM validation.
func (g *Guard) UnaryInterceptor(opts GRPCOptions) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		newCtx, err := g.authenticateGRPC(ctx, opts)
		if err != nil {
			return nil, err
		}
		return handler(newCtx, req)
	}
}

// StreamInterceptor returns a stream gRPC interceptor enforcing IAM validation.
func (g *Guard) StreamInterceptor(opts GRPCOptions) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		newCtx, err := g.authenticateGRPC(ss.Context(), opts)
		if err != nil {
			return err
		}
		wrapped := &contextualStream{ServerStream: ss, ctx: newCtx}
		return handler(srv, wrapped)
	}
}

func (g *Guard) authenticateGRPC(ctx context.Context, opts GRPCOptions) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok && !opts.AllowAnonymous {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}
	token := extractBearer(firstMetadata(md, "authorization"))
	if token == "" {
		if opts.AllowAnonymous {
			return ctx, nil
		}
		return nil, status.Error(codes.Unauthenticated, "missing authorization header")
	}
	if g.validator == nil {
		return nil, status.Error(codes.Internal, "auth validator unavailable")
	}
	claims, err := g.validator.Validate(ctx, token, resolveKind(opts.TokenFormat))
	if err != nil {
		g.logger.Warn("token validation failed", "error", err)
		return nil, status.Error(codes.Unauthenticated, "invalid token")
	}
	if !hasRoles(claims.Roles, opts.RequiredRoles) {
		return nil, status.Error(codes.PermissionDenied, "insufficient role")
	}
	return context.WithValue(ctx, claimsKey{}, claims), nil
}

// ClaimsFromContext fetches IAM claims previously validated.
func ClaimsFromContext(ctx context.Context) (*crypto.Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(*crypto.Claims)
	return claims, ok
}

// WithClaims attaches IAM claims to the provided context (primarily for tests).
func WithClaims(ctx context.Context, claims *crypto.Claims) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if claims == nil {
		return ctx
	}
	return context.WithValue(ctx, claimsKey{}, claims)
}

func resolveKind(kind crypto.TokenKind) crypto.TokenKind {
	if kind == crypto.TokenKindPASETO {
		return crypto.TokenKindPASETO
	}
	return crypto.TokenKindJWT
}

func extractBearer(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func hasRoles(userRoles, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(userRoles))
	for _, role := range userRoles {
		if trimmed := strings.ToLower(strings.TrimSpace(role)); trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	for _, want := range required {
		if _, ok := set[strings.ToLower(strings.TrimSpace(want))]; !ok {
			return false
		}
	}
	return true
}

func firstMetadata(md metadata.MD, key string) string {
	if md == nil {
		return ""
	}
	values := md[strings.ToLower(key)]
	if len(values) == 0 {
		values = md[key]
	}
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

type claimsKey struct{}

type contextualStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *contextualStream) Context() context.Context {
	return s.ctx
}

func writeHTTPError(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":       status,
		"message":    message,
		"request_id": r.Header.Get("X-Request-ID"),
	})
}
