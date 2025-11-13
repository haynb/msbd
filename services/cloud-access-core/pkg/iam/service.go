package iam

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/audit"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
)

// UserRepository represents the subset of storage needed by the IAM service.
type UserRepository interface {
	UserByEmail(ctx context.Context, email string) (*gormdb.User, error)
	UserByID(ctx context.Context, id uuid.UUID) (*gormdb.User, error)
}

// ServiceConfig provides runtime IAM tuning knobs.
type ServiceConfig struct {
	AccessTokenTTL  time.Duration
	DeviceTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	WebAudience     string
	DeviceAudience  string
}

// LoginRequest captures parameters accepted from HTTP/gRPC adapters.
type LoginRequest struct {
	Email             string
	Password          string
	ClientKind        string
	DeviceFingerprint string
	ClientIP          string
}

// AuthResult represents the issued tokens + metadata.
type AuthResult struct {
	SessionID        string
	AccessToken      string
	DeviceToken      string
	RefreshToken     string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	TenantID         uuid.UUID
	UserID           uuid.UUID
	Roles            []string
	Email            string
}

// Service drives IAM authentication/token lifecycle logic.
type Service struct {
	repo     UserRepository
	sessions SessionStore
	signer   *crypto.Signer
	auditor  audit.Recorder
	cfg      ServiceConfig
	logger   *slog.Logger
}

// NewService builds the IAM service.
func NewService(repo UserRepository, sessions SessionStore, signer *crypto.Signer, auditor audit.Recorder, logger *slog.Logger, cfg ServiceConfig) *Service {
	if cfg.AccessTokenTTL <= 0 {
		cfg.AccessTokenTTL = 15 * time.Minute
	}
	if cfg.DeviceTokenTTL <= 0 {
		cfg.DeviceTokenTTL = cfg.AccessTokenTTL
	}
	if cfg.RefreshTokenTTL <= 0 {
		cfg.RefreshTokenTTL = 7 * 24 * time.Hour
	}
	if cfg.DeviceAudience == "" {
		cfg.DeviceAudience = cfg.WebAudience
	}
	return &Service{
		repo:     repo,
		sessions: sessions,
		signer:   signer,
		auditor:  auditor,
		logger:   logger.With("component", "iam_service"),
		cfg:      cfg,
	}
}

// Login validates credentials and issues access/refresh tokens.
func (s *Service) Login(ctx context.Context, req LoginRequest) (*AuthResult, error) {
	start := time.Now()
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || req.Password == "" {
		return nil, ErrInvalidCredentials
	}
	user, err := s.repo.UserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, gormdb.ErrNotFound) {
			s.audit(ctx, audit.Entry{Action: "auth.login", Result: "failed", Metadata: map[string]any{"reason": "not_found", "email": email}})
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if err := s.checkUserState(user); err != nil {
		s.audit(ctx, audit.Entry{TenantID: user.TenantID, ActorID: user.ID, Action: "auth.login", Result: "failed", Metadata: map[string]any{"state": user.State}})
		return nil, err
	}
	if ok, _ := argon2id.ComparePasswordAndHash(req.Password, string(user.PasswordHash)); !ok {
		s.audit(ctx, audit.Entry{TenantID: user.TenantID, ActorID: user.ID, Action: "auth.login", Result: "failed", Metadata: map[string]any{"reason": "bad_password"}})
		return nil, ErrInvalidCredentials
	}
	result, session, err := s.issueTokens(ctx, user, "")
	if err != nil {
		return nil, err
	}
	if err := s.sessions.SaveSession(ctx, session, s.cfg.RefreshTokenTTL); err != nil {
		return nil, fmt.Errorf("persist session: %w", err)
	}
	s.audit(ctx, audit.Entry{
		TenantID: user.TenantID,
		ActorID:  user.ID,
		Action:   "auth.login",
		Result:   "success",
		Latency:  time.Since(start),
		Metadata: map[string]any{"client_kind": req.ClientKind, "session_id": result.SessionID, "client_ip": req.ClientIP},
	})
	return result, nil
}

// Refresh exchanges a refresh token for new access credentials.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*AuthResult, error) {
	if refreshToken == "" {
		return nil, ErrInvalidCredentials
	}
	session, err := s.sessions.SessionByRefreshToken(ctx, refreshToken)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if time.Now().UTC().After(session.RefreshExpiresAt) {
		return nil, ErrSessionExpired
	}
	userID, err := uuid.Parse(session.UserID)
	if err != nil {
		return nil, ErrSessionExpired
	}
	user, err := s.repo.UserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("reload user: %w", err)
	}
	if err := s.checkUserState(user); err != nil {
		return nil, err
	}
	result, newSession, err := s.issueTokens(ctx, user, session.ID)
	if err != nil {
		return nil, err
	}
	if err := s.sessions.DeleteSession(ctx, session.ID, refreshToken); err != nil {
		s.logger.Warn("failed to delete old session", "error", err)
	}
	if err := s.sessions.SaveSession(ctx, newSession, s.cfg.RefreshTokenTTL); err != nil {
		return nil, fmt.Errorf("persist session: %w", err)
	}
	s.audit(ctx, audit.Entry{
		TenantID: user.TenantID,
		ActorID:  user.ID,
		Action:   "auth.refresh",
		Result:   "success",
		Metadata: map[string]any{"session_id": result.SessionID},
	})
	return result, nil
}

// Logout revokes the session associated with the refresh token.
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	session, err := s.sessions.SessionByRefreshToken(ctx, refreshToken)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil
		}
		return err
	}
	if err := s.sessions.DeleteSession(ctx, session.ID, refreshToken); err != nil {
		return err
	}
	tenantID, _ := uuid.Parse(session.TenantID)
	actorID, _ := uuid.Parse(session.UserID)
	s.audit(ctx, audit.Entry{TenantID: tenantID, ActorID: actorID, Action: "auth.logout", Result: "success", Metadata: map[string]any{"session_id": session.ID}})
	return nil
}

// Validate verifies a token of the requested kind and returns its claims.
func (s *Service) Validate(ctx context.Context, token string, kind crypto.TokenKind) (*crypto.Claims, error) {
	if token == "" {
		return nil, ErrInvalidCredentials
	}
	var (
		claims *crypto.Claims
		err    error
	)
	switch kind {
	case crypto.TokenKindPASETO:
		claims, err = s.signer.VerifyPASETO(token, s.cfg.DeviceAudience)
	case crypto.TokenKindJWT:
		fallthrough
	default:
		claims, err = s.signer.VerifyJWT(token, s.cfg.WebAudience)
	}
	if err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	return claims, nil
}

func (s *Service) issueTokens(ctx context.Context, user *gormdb.User, sessionID string) (*AuthResult, *Session, error) {
	if sessionID == "" {
		sessionID = uuid.NewString()
	}
	refreshToken, err := generateRefreshToken()
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	roles := append([]string(nil), []string(user.Roles)...)
	claims := crypto.Claims{
		Subject:   user.ID.String(),
		TenantID:  user.TenantID.String(),
		SessionID: sessionID,
		Email:     user.Email,
		Roles:     roles,
	}
	accessToken, err := s.signer.SignJWT(claims, s.cfg.WebAudience, s.cfg.AccessTokenTTL)
	if err != nil {
		return nil, nil, err
	}
	deviceToken, err := s.signer.SignPASETO(claims, s.cfg.DeviceAudience, s.cfg.DeviceTokenTTL)
	if err != nil {
		return nil, nil, err
	}
	session := &Session{
		ID:               sessionID,
		UserID:           user.ID.String(),
		TenantID:         user.TenantID.String(),
		Email:            user.Email,
		Roles:            roles,
		RefreshTokenHash: hashRefreshToken(refreshToken),
		IssuedAt:         now,
		ExpiresAt:        accessToken.ExpiresAt,
		RefreshExpiresAt: now.Add(s.cfg.RefreshTokenTTL),
	}
	result := &AuthResult{
		SessionID:        sessionID,
		AccessToken:      accessToken.Value,
		DeviceToken:      deviceToken.Value,
		RefreshToken:     refreshToken,
		AccessExpiresAt:  accessToken.ExpiresAt,
		RefreshExpiresAt: session.RefreshExpiresAt,
		TenantID:         user.TenantID,
		UserID:           user.ID,
		Roles:            roles,
		Email:            user.Email,
	}
	return result, session, nil
}

func generateRefreshToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (s *Service) checkUserState(user *gormdb.User) error {
	switch strings.ToLower(user.State) {
	case "frozen":
		return ErrAccountFrozen
	default:
		return nil
	}
}

func (s *Service) audit(ctx context.Context, entry audit.Entry) {
	if s.auditor == nil {
		return
	}
	s.auditor.Record(ctx, entry)
}

// NewInMemorySessionStore exposes an in-memory store for unit tests.
func NewInMemorySessionStore() SessionStore {
	return newMemorySessionStore()
}
