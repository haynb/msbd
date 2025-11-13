package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TokenFetcher retrieves short-lived provider tokens.
type TokenFetcher interface {
	Fetch(ctx context.Context) (TokenResult, error)
}

// TokenResult captures a fetched token value.
type TokenResult struct {
	Token     string
	ExpiresAt time.Time
	ExpiresIn time.Duration
}

// TokenCache memoizes Aliyun tokens to avoid redundant refreshes.
type TokenCache struct {
	mu            sync.RWMutex
	token         string
	expiresAt     time.Time
	fetcher       TokenFetcher
	refreshWindow time.Duration
}

// NewTokenCache constructs a cache with optional fetcher.
func NewTokenCache(initial string, fetcher TokenFetcher, refreshWindow time.Duration) *TokenCache {
	if refreshWindow <= 0 {
		refreshWindow = 60 * time.Second
	}
	cache := &TokenCache{fetcher: fetcher, refreshWindow: refreshWindow}
	if initial != "" {
		cache.token = initial
		cache.expiresAt = time.Now().Add(24 * time.Hour)
	}
	return cache
}

// Token returns a valid token, refreshing if necessary.
func (c *TokenCache) Token(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.RLock()
	token := c.token
	expires := c.expiresAt
	c.mu.RUnlock()
	if token != "" && time.Until(expires) > c.refreshWindow {
		return token, nil
	}
	return c.refresh(ctx)
}

func (c *TokenCache) refresh(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.expiresAt) > c.refreshWindow {
		return c.token, nil
	}
	if c.fetcher == nil {
		if c.token == "" {
			return "", errors.New("aliyun token not configured")
		}
		c.expiresAt = time.Now().Add(10 * time.Minute)
		return c.token, nil
	}
	result, err := c.fetcher.Fetch(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(result.Token) == "" {
		return "", errors.New("fetched empty aliyun token")
	}
	token := strings.TrimSpace(result.Token)
	expires := result.ExpiresAt
	if expires.IsZero() {
		if result.ExpiresIn <= 0 {
			result.ExpiresIn = 10 * time.Minute
		}
		expires = time.Now().Add(result.ExpiresIn)
	}
	c.token = token
	c.expiresAt = expires
	return token, nil
}

// HTTPTokenFetcher loads tokens from an HTTP endpoint returning JSON.
type HTTPTokenFetcher struct {
	URL    string
	Client *http.Client
	Header http.Header
}

// Fetch implements TokenFetcher.
func (f *HTTPTokenFetcher) Fetch(ctx context.Context) (TokenResult, error) {
	if f == nil || strings.TrimSpace(f.URL) == "" {
		return TokenResult{}, errors.New("token url not configured")
	}
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return TokenResult{}, err
	}
	for k, values := range f.Header {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return TokenResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return TokenResult{}, fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}
	var payload tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return TokenResult{}, err
	}
	return payload.toResult()
}

type tokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
	ExpireIn  int64  `json:"expire_in"`
	ExpiresAt string `json:"expires_at"`
	ExpireAt  string `json:"expire_at"`
}

func (r tokenResponse) toResult() (TokenResult, error) {
	res := TokenResult{Token: strings.TrimSpace(r.Token)}
	if res.Token == "" {
		return TokenResult{}, errors.New("token response missing token")
	}
	if ts := strings.TrimSpace(r.ExpiresAt); ts != "" {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			res.ExpiresAt = parsed
		}
	}
	if res.ExpiresAt.IsZero() {
		if ts := strings.TrimSpace(r.ExpireAt); ts != "" {
			if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
				res.ExpiresAt = parsed
			}
		}
	}
	seconds := r.ExpiresIn
	if seconds == 0 {
		seconds = r.ExpireIn
	}
	if seconds > 0 {
		res.ExpiresIn = time.Duration(seconds) * time.Second
		if res.ExpiresAt.IsZero() {
			res.ExpiresAt = time.Now().Add(res.ExpiresIn)
		}
	}
	return res, nil
}
