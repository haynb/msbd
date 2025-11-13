package iam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Session captures refresh/session lifecycle metadata persisted in Redis.
type Session struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	TenantID         string    `json:"tenant_id"`
	Email            string    `json:"email"`
	Roles            []string  `json:"roles"`
	RefreshTokenHash string    `json:"refresh_token_hash"`
	IssuedAt         time.Time `json:"issued_at"`
	ExpiresAt        time.Time `json:"expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

// SessionStore abstracts refresh token persistence for revocation.
type SessionStore interface {
	SaveSession(ctx context.Context, session *Session, ttl time.Duration) error
	SessionByRefreshToken(ctx context.Context, refreshToken string) (*Session, error)
	DeleteSession(ctx context.Context, sessionID string, refreshToken string) error
}

// NewRedisSessionStore builds a redis-backed session store.
func NewRedisSessionStore(client *redis.Client, prefix string) SessionStore {
	if prefix == "" {
		prefix = "cloud-access-core"
	}
	return &redisSessionStore{client: client, prefix: prefix}
}

type redisSessionStore struct {
	client *redis.Client
	prefix string
}

func (s *redisSessionStore) SaveSession(ctx context.Context, session *Session, ttl time.Duration) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	refreshKey := s.refreshKey(session.RefreshTokenHash)
	sessionKey := s.sessionKey(session.ID)
	if ttl <= 0 {
		ttl = time.Minute
	}
	_, err = s.client.Pipelined(ctx, func(p redis.Pipeliner) error {
		p.Set(ctx, sessionKey, payload, ttl)
		p.Set(ctx, refreshKey, session.ID, ttl)
		return nil
	})
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func (s *redisSessionStore) SessionByRefreshToken(ctx context.Context, refreshToken string) (*Session, error) {
	refreshHash := hashRefreshToken(refreshToken)
	sessionID, err := s.client.Get(ctx, s.refreshKey(refreshHash)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("lookup refresh session: %w", err)
	}
	data, err := s.client.Get(ctx, s.sessionKey(sessionID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("load session payload: %w", err)
	}
	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	if session.RefreshTokenHash != refreshHash {
		return nil, ErrSessionNotFound
	}
	return &session, nil
}

func (s *redisSessionStore) DeleteSession(ctx context.Context, sessionID string, refreshToken string) error {
	refreshHash := hashRefreshToken(refreshToken)
	_, err := s.client.Pipelined(ctx, func(p redis.Pipeliner) error {
		p.Del(ctx, s.sessionKey(sessionID))
		p.Del(ctx, s.refreshKey(refreshHash))
		return nil
	})
	return err
}

func (s *redisSessionStore) sessionKey(id string) string {
	return fmt.Sprintf("%s:session:%s", s.prefix, id)
}

func (s *redisSessionStore) refreshKey(hash string) string {
	return fmt.Sprintf("%s:refresh:%s", s.prefix, hash)
}

func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// memorySessionStore is used in tests when Redis is not available.
type memorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	byHash   map[string]string
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{
		sessions: make(map[string]*Session),
		byHash:   make(map[string]string),
	}
}

func (m *memorySessionStore) SaveSession(_ context.Context, session *Session, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.ID] = session
	m.byHash[session.RefreshTokenHash] = session.ID
	return nil
}

func (m *memorySessionStore) SessionByRefreshToken(_ context.Context, refreshToken string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if sessionID, ok := m.byHash[hashRefreshToken(refreshToken)]; ok {
		if session, exists := m.sessions[sessionID]; exists {
			return session, nil
		}
	}
	return nil, ErrSessionNotFound
}

func (m *memorySessionStore) DeleteSession(_ context.Context, sessionID string, refreshToken string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
	delete(m.byHash, hashRefreshToken(refreshToken))
	return nil
}
