package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
)

type claimsKey struct{}

func withClaims(ctx context.Context, claims *crypto.Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, claims)
}

// ClaimsFromContext fetches IAM claims stored within the request context.
func ClaimsFromContext(ctx context.Context) (*crypto.Claims, bool) {
	claims, ok := ctx.Value(claimsKey{}).(*crypto.Claims)
	return claims, ok
}

func extractBearerToken(r *http.Request) (string, error) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if auth == "" {
		return "", errors.New("missing authorization header")
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", errors.New("invalid authorization header")
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", errors.New("invalid authorization header")
	}
	return token, nil
}

func tokenKind(format string) crypto.TokenKind {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "paseto":
		return crypto.TokenKindPASETO
	default:
		return crypto.TokenKindJWT
	}
}

func hasRole(userRoles []string, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(userRoles))
	for _, role := range userRoles {
		if trimmed := strings.TrimSpace(strings.ToLower(role)); trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	for _, want := range required {
		if _, ok := set[want]; !ok {
			return false
		}
	}
	return true
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

type rateLimitError struct {
	RetryAfter time.Duration
}

func (r *rateLimitError) Error() string {
	return "rate limit exceeded"
}

func writeGatewayError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":       code,
		"message":    message,
		"request_id": r.Header.Get("X-Request-ID"),
	})
}
