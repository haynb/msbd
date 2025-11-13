package crypto

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenKind represents the concrete token format.
type TokenKind string

const (
	// TokenKindJWT is used for web/browser flows.
	TokenKindJWT TokenKind = "jwt"
	// TokenKindPASETO is used for desktop/device flows.
	TokenKindPASETO TokenKind = "paseto"
)

// Claims describe the common IAM fields embedded into every token.
type Claims struct {
	Subject   string
	TenantID  string
	SessionID string
	Email     string
	Roles     []string
	TokenID   string
}

// SignedToken bundles the serialized token with metadata.
type SignedToken struct {
	Value     string
	TokenID   string
	ExpiresAt time.Time
}

// Options configure the signer instance.
type Options struct {
	Issuer           string
	KeyID            string
	PrivateKeyBase64 string
	PublicKeyBase64  string
}

// Signer issues and validates JWT + PASETO tokens using a shared Ed25519 key pair.
type Signer struct {
	issuer       string
	keyID        string
	edPrivate    ed25519.PrivateKey
	edPublic     ed25519.PublicKey
	pasetoSecret paseto.V4AsymmetricSecretKey
	pasetoPublic paseto.V4AsymmetricPublicKey
}

// NewSigner constructs a signer from base64 encoded Ed25519 keys (or generates one for dev).
func NewSigner(opts Options) (*Signer, error) {
	secret, public, edPriv, edPub, err := loadOrGenerateKeys(opts)
	if err != nil {
		return nil, err
	}
	if opts.Issuer == "" {
		return nil, errors.New("issuer is required")
	}
	return &Signer{
		issuer:       opts.Issuer,
		keyID:        opts.KeyID,
		edPrivate:    edPriv,
		edPublic:     edPub,
		pasetoSecret: secret,
		pasetoPublic: public,
	}, nil
}

func loadOrGenerateKeys(opts Options) (paseto.V4AsymmetricSecretKey, paseto.V4AsymmetricPublicKey, ed25519.PrivateKey, ed25519.PublicKey, error) {
	if opts.PrivateKeyBase64 != "" && opts.PublicKeyBase64 != "" {
		privBytes, err := base64.StdEncoding.DecodeString(opts.PrivateKeyBase64)
		if err != nil {
			return paseto.V4AsymmetricSecretKey{}, paseto.V4AsymmetricPublicKey{}, nil, nil, fmt.Errorf("decode private key: %w", err)
		}
		pubBytes, err := base64.StdEncoding.DecodeString(opts.PublicKeyBase64)
		if err != nil {
			return paseto.V4AsymmetricSecretKey{}, paseto.V4AsymmetricPublicKey{}, nil, nil, fmt.Errorf("decode public key: %w", err)
		}
		secret, err := paseto.NewV4AsymmetricSecretKeyFromBytes(privBytes)
		if err != nil {
			return paseto.V4AsymmetricSecretKey{}, paseto.V4AsymmetricPublicKey{}, nil, nil, fmt.Errorf("load secret key: %w", err)
		}
		public, err := paseto.NewV4AsymmetricPublicKeyFromBytes(pubBytes)
		if err != nil {
			return paseto.V4AsymmetricSecretKey{}, paseto.V4AsymmetricPublicKey{}, nil, nil, fmt.Errorf("load public key: %w", err)
		}
		return secret, public, ed25519.PrivateKey(privBytes), ed25519.PublicKey(pubBytes), nil
	}

	// Development convenience: generate ephemeral key pair.
	secret := paseto.NewV4AsymmetricSecretKey()
	public := secret.Public()
	privBytes := secret.ExportBytes()
	pubBytes := public.ExportBytes()
	return secret, public, ed25519.PrivateKey(privBytes), ed25519.PublicKey(pubBytes), nil
}

// SignJWT issues a JWT for the provided claims/audience/TTL.
func (s *Signer) SignJWT(claims Claims, audience string, ttl time.Duration) (*SignedToken, error) {
	now := time.Now().UTC()
	jid := uuid.NewString()
	expires := now.Add(ttl)
	mapClaims := jwt.MapClaims{
		"iss":        s.issuer,
		"sub":        claims.Subject,
		"aud":        audience,
		"exp":        expires.Unix(),
		"iat":        now.Unix(),
		"jti":        jid,
		"tenant_id":  claims.TenantID,
		"session_id": claims.SessionID,
		"email":      claims.Email,
		"roles":      claims.Roles,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, mapClaims)
	if s.keyID != "" {
		token.Header["kid"] = s.keyID
	}
	signed, err := token.SignedString(s.edPrivate)
	if err != nil {
		return nil, fmt.Errorf("sign jwt: %w", err)
	}
	return &SignedToken{Value: signed, TokenID: jid, ExpiresAt: expires}, nil
}

// SignPASETO issues a v4 public PASETO token.
func (s *Signer) SignPASETO(claims Claims, audience string, ttl time.Duration) (*SignedToken, error) {
	now := time.Now().UTC()
	expires := now.Add(ttl)
	jid := uuid.NewString()

	token := paseto.NewToken()
	token.SetIssuer(s.issuer)
	token.SetAudience(audience)
	token.SetSubject(claims.Subject)
	token.SetIssuedAt(now)
	token.SetNotBefore(now)
	token.SetExpiration(expires)
	token.SetJti(jid)
	token.SetString("tenant_id", claims.TenantID)
	token.SetString("session_id", claims.SessionID)
	token.SetString("email", claims.Email)
	if err := token.Set("roles", claims.Roles); err != nil {
		return nil, fmt.Errorf("set paseto roles: %w", err)
	}
	if s.keyID != "" {
		footer, _ := json.Marshal(map[string]string{"kid": s.keyID})
		token.SetFooter(footer)
	}
	signed := token.V4Sign(s.pasetoSecret, nil)
	return &SignedToken{Value: signed, TokenID: jid, ExpiresAt: expires}, nil
}

// VerifyJWT validates and extracts claims from a JWT string.
func (s *Signer) VerifyJWT(token string, audience string) (*Claims, error) {
	parsed, err := jwt.Parse(token, func(tok *jwt.Token) (interface{}, error) {
		if _, ok := tok.Method.(*jwt.SigningMethodEd25519); !ok {
			return nil, fmt.Errorf("unexpected jwt signing method %T", tok.Method)
		}
		return s.edPublic, nil
	}, jwt.WithAudience(audience), jwt.WithIssuer(s.issuer))
	if err != nil {
		return nil, fmt.Errorf("verify jwt: %w", err)
	}
	return claimsFromJWT(parsed.Claims)
}

// VerifyPASETO validates and extracts claims from a v4 public token.
func (s *Signer) VerifyPASETO(token string, audience string) (*Claims, error) {
	parser := paseto.NewParser()
	parser.AddRule(paseto.IssuedBy(s.issuer))
	if audience != "" {
		parser.AddRule(paseto.ForAudience(audience))
	}
	parsed, err := parser.ParseV4Public(s.pasetoPublic, token, nil)
	if err != nil {
		return nil, fmt.Errorf("verify paseto: %w", err)
	}
	return claimsFromPaseto(parsed)
}

func claimsFromJWT(raw jwt.Claims) (*Claims, error) {
	mapClaims, ok := raw.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid jwt claims type")
	}
	return &Claims{
		Subject:   stringFromClaims(mapClaims["sub"]),
		TenantID:  stringFromClaims(mapClaims["tenant_id"]),
		SessionID: stringFromClaims(mapClaims["session_id"]),
		Email:     stringFromClaims(mapClaims["email"]),
		Roles:     stringSliceFromClaims(mapClaims["roles"]),
		TokenID:   stringFromClaims(mapClaims["jti"]),
	}, nil
}

func claimsFromPaseto(token *paseto.Token) (*Claims, error) {
	subject, _ := token.GetSubject()
	tenantID, _ := token.GetString("tenant_id")
	sessionID, _ := token.GetString("session_id")
	email, _ := token.GetString("email")
	jti, _ := token.GetString("jti")
	var roles []string
	if err := token.Get("roles", &roles); err != nil {
		roles = nil
	}
	return &Claims{
		Subject:   subject,
		TenantID:  tenantID,
		SessionID: sessionID,
		Email:     email,
		Roles:     roles,
		TokenID:   jti,
	}, nil
}

func stringFromClaims(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func stringSliceFromClaims(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			result = append(result, stringFromClaims(item))
		}
		return result
	case nil:
		return nil
	default:
		return nil
	}
}
