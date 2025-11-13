package speechgateway

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/rate"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/metrics"
)

// Service coordinates active speech streams across providers.
type Service struct {
	cfg             bootstrap.SpeechGatewayConfig
	baseDefault     string
	defaultOverride atomic.Value
	providers       map[string]Provider
	providersMu     sync.RWMutex
	redis           redis.Cmdable
	bucket          rate.TokenBucket
	breaker         *breaker
	logger          *slog.Logger
	metrics         *metrics.Collector
	now             func() time.Time
}

// GatewayStatus captures the aggregated health for provider routing.
type GatewayStatus struct {
	DefaultProvider string         `json:"default_provider"`
	Providers       []ProviderInfo `json:"providers"`
	CircuitOpen     bool           `json:"circuit_open"`
	FailureCount    int            `json:"failure_count"`
	ResetAt         *time.Time     `json:"reset_at,omitempty"`
}

// ProviderInfo describes each registered adapter.
type ProviderInfo struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// NewService constructs a speech gateway service.
func NewService(cfg bootstrap.SpeechGatewayConfig, defaultProvider string, redisClient redis.Cmdable, bucket rate.TokenBucket, metricsCollector *metrics.Collector, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(defaultProvider) == "" {
		defaultProvider = "aliyun"
	}
	svc := &Service{
		cfg:         cfg,
		baseDefault: strings.ToLower(defaultProvider),
		providers:   make(map[string]Provider),
		redis:       redisClient,
		bucket:      bucket,
		breaker:     newBreaker(cfg.CircuitBreakerFailures, cfg.CircuitReset()),
		logger:      logger.With("component", "speechgateway"),
		metrics:     metricsCollector,
		now:         time.Now,
	}
	svc.defaultOverride.Store("")
	return svc
}

// RegisterProvider registers an adapter under the provided key.
func (s *Service) RegisterProvider(name string, provider Provider) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" || provider == nil {
		return
	}
	s.providersMu.Lock()
	defer s.providersMu.Unlock()
	s.providers[name] = provider
}

// Status returns a best-effort snapshot of the gateway health.
func (s *Service) Status() GatewayStatus {
	s.providersMu.RLock()
	providers := make([]ProviderInfo, 0, len(s.providers))
	for name := range s.providers {
		providers = append(providers, ProviderInfo{Name: name, Default: name == s.currentDefaultProvider()})
	}
	s.providersMu.RUnlock()
	state := breakerState{}
	if s.breaker != nil {
		state = s.breaker.State(s.now())
	}
	status := GatewayStatus{
		DefaultProvider: s.currentDefaultProvider(),
		Providers:       providers,
		CircuitOpen:     state.Open,
		FailureCount:    state.Failures,
	}
	if !state.ResetAt.IsZero() {
		reset := state.ResetAt.UTC()
		status.ResetAt = &reset
	}
	return status
}

// SetDefaultProvider overrides the default provider used for new sessions.
func (s *Service) SetDefaultProvider(name string) {
	if s == nil {
		return
	}
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" || trimmed == s.baseDefault {
		s.defaultOverride.Store("")
		return
	}
	s.defaultOverride.Store(trimmed)
}

func (s *Service) currentDefaultProvider() string {
	if override, ok := s.defaultOverride.Load().(string); ok && strings.TrimSpace(override) != "" {
		return override
	}
	return s.baseDefault
}

// SetClock overrides the internal clock (primarily for tests).
func (s *Service) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// OpenStream negotiates a provider session.
func (s *Service) OpenStream(ctx context.Context, req StartRequest) (*Session, error) {
	if !s.cfg.Enabled {
		return nil, errors.New("speech gateway disabled")
	}
	selected, providerName, err := s.selectProvider(req.Provider)
	if err != nil {
		return nil, err
	}
	if req.SessionID == uuid.Nil {
		req.SessionID = uuid.New()
	}
	if req.SampleRate <= 0 {
		req.SampleRate = 16000
	}
	if strings.TrimSpace(req.Format) == "" {
		req.Format = "pcm"
	}
	if req.AckDeadline <= 0 {
		req.AckDeadline = s.cfg.AckDeadline()
	}
	if err := s.allowStream(ctx, req.TenantID); err != nil {
		return nil, err
	}
	if !s.breaker.Allow(s.now()) {
		return nil, ErrProviderUnavailable
	}
	handle, err := selected.Start(ctx, req)
	if err != nil {
		s.breaker.Fail(s.now())
		s.observeBreaker()
		return nil, err
	}
	s.breaker.Success()
	s.observeBreaker()

	sessionCtx, cancel := context.WithCancel(ctx)
	out := make(chan ProviderEvent, 64)
	in := handle.Events()
	if in == nil {
		close(out)
	} else {
		go s.forwardEvents(sessionCtx, out, in)
	}

	session := &Session{
		id:       req.SessionID,
		tenantID: req.TenantID,
		userID:   req.UserID,
		provider: providerName,
		handle:   handle,
		events:   out,
		ack: SessionAck{
			SessionID:  req.SessionID,
			Provider:   providerName,
			SampleRate: req.SampleRate,
			Format:     req.Format,
		},
		service:      s,
		lastSeq:      -1,
		lastActivity: s.now(),
		cancel:       cancel,
	}
	return session, nil
}

func (s *Service) forwardEvents(ctx context.Context, out chan<- ProviderEvent, in <-chan ProviderEvent) {
	defer close(out)
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-in:
			if !ok {
				return
			}
			if evt.Timestamp.IsZero() {
				evt.Timestamp = s.now()
			}
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (s *Service) selectProvider(name string) (Provider, string, error) {
	s.providersMu.RLock()
	defer s.providersMu.RUnlock()
	key := strings.TrimSpace(strings.ToLower(name))
	if key == "" {
		key = s.currentDefaultProvider()
	}
	provider, ok := s.providers[key]
	if !ok {
		return nil, "", fmt.Errorf("provider %s not registered", key)
	}
	return provider, key, nil
}

func (s *Service) allowStream(ctx context.Context, tenant uuid.UUID) error {
	if s.bucket == nil || tenant == uuid.Nil {
		return nil
	}
	res, err := s.bucket.Take(ctx, fmt.Sprintf("speech:%s", tenant.String()), 120, 4, 1, time.Minute)
	if err != nil {
		s.logger.Warn("token bucket failed", "error", err)
		return nil
	}
	if !res.Allowed {
		return RateLimitError{RetryAfter: res.RetryAfter}
	}
	return nil
}

func (s *Service) sendChunk(ctx context.Context, session *Session, chunk AudioChunk) error {
	if len(chunk.Data) == 0 && !chunk.EndOfStream {
		return ErrInvalidChunk
	}
	if len(chunk.Data) > 0 && len(chunk.Data) > s.cfg.MaxChunkBytes {
		return fmt.Errorf("%w: max %d bytes", ErrInvalidChunk, s.cfg.MaxChunkBytes)
	}
	if chunk.ReceivedAt.IsZero() {
		chunk.ReceivedAt = s.now()
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed.Load() {
		return ErrInvalidChunk
	}
	if session.lastSeq >= 0 && chunk.Sequence <= session.lastSeq {
		return ErrOutOfOrder
	}
	if idle := chunk.ReceivedAt.Sub(session.lastActivity); idle > s.cfg.SessionIdleTimeout() {
		return fmt.Errorf("%w: idle timeout", ErrInvalidChunk)
	}
	session.lastSeq = chunk.Sequence
	session.lastActivity = chunk.ReceivedAt
	if err := session.handle.SendAudio(ctx, chunk); err != nil {
		s.logger.Warn("provider send failed", "session", session.id, "seq", chunk.Sequence, "error", err)
		s.bufferChunk(ctx, session.id, chunk.Sequence, chunk.Data)
		s.breaker.Fail(s.now())
		s.observeBreaker()
		s.recordProviderError(session.provider)
		return err
	}
	return nil
}

func (s *Service) closeSession(ctx context.Context, session *Session) error {
	if session.closed.Swap(true) {
		return nil
	}
	session.cancel()
	if session.handle == nil {
		return nil
	}
	if err := session.handle.Close(ctx); err != nil {
		s.logger.Warn("provider close failed", "session", session.id, "error", err)
		return err
	}
	return nil
}

func (s *Service) bufferChunk(ctx context.Context, sessionID uuid.UUID, seq int64, data []byte) {
	if s.redis == nil || len(data) == 0 || strings.TrimSpace(s.cfg.BufferPrefix) == "" {
		return
	}
	key := fmt.Sprintf("%s:%s", strings.TrimSuffix(s.cfg.BufferPrefix, ":"), sessionID.String())
	payload := data
	if len(payload) > 2048 {
		payload = payload[:2048]
	}
	encoded := base64.StdEncoding.EncodeToString(payload)
	if err := s.redis.HSet(ctx, key, fmt.Sprintf("%d", seq), encoded).Err(); err == nil {
		_ = s.redis.Expire(ctx, key, s.cfg.BufferTTL())
	}
}

// Session represents an active speech stream.
type Session struct {
	id           uuid.UUID
	tenantID     uuid.UUID
	userID       uuid.UUID
	provider     string
	handle       StreamHandle
	events       <-chan ProviderEvent
	ack          SessionAck
	service      *Service
	lastSeq      int64
	lastActivity time.Time
	closed       atomic.Bool
	cancel       context.CancelFunc
	mu           sync.Mutex
}

// ID returns the stream session ID.
func (s *Session) ID() uuid.UUID { return s.id }

// Ack returns the negotiated session acknowledgement.
func (s *Session) Ack() SessionAck { return s.ack }

// Events exposes provider callbacks.
func (s *Session) Events() <-chan ProviderEvent { return s.events }

// SendChunk forwards PCM payloads to the provider.
func (s *Session) SendChunk(ctx context.Context, chunk AudioChunk) error {
	return s.service.sendChunk(ctx, s, chunk)
}

// Close terminates the provider stream.
func (s *Session) Close(ctx context.Context) error {
	return s.service.closeSession(ctx, s)
}

// TenantID returns the tenant associated with the stream.
func (s *Session) TenantID() uuid.UUID { return s.tenantID }

// UserID returns the requesting user for the stream.
func (s *Session) UserID() uuid.UUID { return s.userID }

// ProviderName exposes the provider key for this session.
func (s *Session) ProviderName() string { return s.provider }

type breaker struct {
	mu        sync.Mutex
	failures  int
	threshold int
	openedAt  time.Time
	reset     time.Duration
}

type breakerState struct {
	Open     bool
	Failures int
	ResetAt  time.Time
}

func newBreaker(threshold int, reset time.Duration) *breaker {
	if threshold <= 0 {
		threshold = 5
	}
	if reset <= 0 {
		reset = 30 * time.Second
	}
	return &breaker{threshold: threshold, reset: reset}
}

func (b *breaker) Allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openedAt.IsZero() {
		return true
	}
	if now.Sub(b.openedAt) >= b.reset {
		b.failures = 0
		b.openedAt = time.Time{}
		return true
	}
	return false
}

func (b *breaker) Fail(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.threshold {
		b.openedAt = now
	}
}

func (b *breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.openedAt = time.Time{}
}

func (b *breaker) State(now time.Time) breakerState {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := breakerState{Failures: b.failures}
	if !b.openedAt.IsZero() {
		state.Open = true
		state.ResetAt = b.openedAt.Add(b.reset)
		if now.Sub(b.openedAt) >= b.reset {
			state.Open = false
			state.ResetAt = time.Time{}
		}
	}
	return state
}

func (s *Service) observeBreaker() {
	if s == nil || s.metrics == nil || s.breaker == nil {
		return
	}
	state := s.breaker.State(s.now())
	s.metrics.SetSpeechCircuit(state.Open, state.Failures)
}

func (s *Service) recordProviderError(provider string) {
	if s == nil || s.metrics == nil {
		return
	}
	s.metrics.RecordProviderError(provider)
}
