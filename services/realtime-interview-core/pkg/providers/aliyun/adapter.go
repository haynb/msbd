package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	nls "github.com/aliyun/alibabacloud-nls-go-sdk"
	"github.com/google/uuid"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/speechgateway"
)

// Adapter wraps the Aliyun NLS Go SDK behind the Provider interface.
type Adapter struct {
	mu         sync.RWMutex
	cfg        bootstrap.AliyunProviderConfig
	tokenCache *TokenCache
	factory    speechFactory
	logger     *slog.Logger
}

// NewAdapter constructs a default Aliyun adapter.
func NewAdapter(cfg bootstrap.AliyunProviderConfig, cache *TokenCache, logger *slog.Logger) *Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	if cache == nil {
		cache = newTokenCacheFromConfig(cfg)
	}
	return &Adapter{
		cfg:        cfg,
		tokenCache: cache,
		factory:    defaultFactory{},
		logger:     logger.With("adapter", "aliyun"),
	}
}

// UpdateConfig swaps the adapter config at runtime.
func (a *Adapter) UpdateConfig(cfg bootstrap.AliyunProviderConfig) error {
	if a == nil {
		return errors.New("adapter nil")
	}
	if strings.TrimSpace(cfg.AppKey) == "" {
		return errors.New("aliyun app_key required")
	}
	cache := newTokenCacheFromConfig(cfg)
	a.mu.Lock()
	a.cfg = cfg
	a.tokenCache = cache
	a.mu.Unlock()
	return nil
}

func newTokenCacheFromConfig(cfg bootstrap.AliyunProviderConfig) *TokenCache {
	var fetcher TokenFetcher
	if strings.TrimSpace(cfg.TokenURL) != "" {
		fetcher = &HTTPTokenFetcher{URL: cfg.TokenURL}
	}
	return NewTokenCache(cfg.Token, fetcher, 60*time.Second)
}

// Start implements speechgateway.Provider.
func (a *Adapter) Start(ctx context.Context, params speechgateway.StartRequest) (speechgateway.StreamHandle, error) {
	cfg, cache := a.currentState()
	if strings.TrimSpace(cfg.AppKey) == "" {
		return nil, errors.New("aliyun app_key required")
	}
	if cache == nil {
		return nil, errors.New("aliyun token cache not configured")
	}
	token, err := cache.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetch aliyun token: %w", err)
	}
	connCfg := nls.NewConnectionConfigWithToken(a.endpoint(cfg), cfg.AppKey, token)
	sdkLogger := nls.NewNlsLogger(io.Discard, "aliyun", 0)
	sdkLogger.SetLogSil(true)

	providerName := strings.TrimSpace(params.Provider)
	if providerName == "" {
		providerName = "aliyun"
	}
	sessCtx := newSessionContext(providerName, a.logger)
	handlers := handlerBundle{
		taskFailed:    a.handleTaskFailed,
		started:       a.handleSentence,
		sentenceBegin: a.handleSentence,
		sentenceEnd:   a.handleSentence,
		resultChanged: a.handleSentence,
		completed:     a.handleSentence,
		closed:        a.handleClose,
		param:         sessCtx,
	}
	st, err := a.factory.New(connCfg, sdkLogger, handlers)
	if err != nil {
		sessCtx.close()
		return nil, err
	}
	startParam := a.startParam(cfg, params)
	extra := a.extraParams(cfg, params)
	ready, err := st.Start(startParam, extra)
	if err != nil {
		st.Shutdown()
		sessCtx.close()
		return nil, err
	}
	deadline := params.AckDeadline
	if deadline <= 0 {
		deadline = 2 * time.Second
	}
	if ready != nil {
		select {
		case ok := <-ready:
			if !ok {
				st.Shutdown()
				sessCtx.close()
				return nil, errors.New("aliyun handshake rejected")
			}
		case <-ctx.Done():
			st.Shutdown()
			sessCtx.close()
			return nil, ctx.Err()
		case <-time.After(deadline):
			st.Shutdown()
			sessCtx.close()
			return nil, errors.New("aliyun handshake timeout")
		}
	}
	return &aliyunHandle{st: st, ctx: sessCtx, logger: a.logger}, nil
}

func (a *Adapter) endpoint(cfg bootstrap.AliyunProviderConfig) string {
	if strings.TrimSpace(cfg.APIURL) == "" {
		return nls.DEFAULT_URL
	}
	return cfg.APIURL
}

func (a *Adapter) startParam(cfg bootstrap.AliyunProviderConfig, req speechgateway.StartRequest) nls.SpeechTranscriptionStartParam {
	param := nls.DefaultSpeechTranscriptionParam()
	if strings.TrimSpace(req.Format) != "" {
		param.Format = req.Format
	} else if strings.TrimSpace(cfg.Format) != "" {
		param.Format = cfg.Format
	}
	if req.SampleRate > 0 {
		param.SampleRate = req.SampleRate
	} else if cfg.SampleRate > 0 {
		param.SampleRate = cfg.SampleRate
	}
	param.EnableIntermediateResult = cfg.EnableIntermediateResult
	param.EnableInverseTextNormalization = cfg.EnableInverseTextNormalization
	param.EnablePunctuationPrediction = cfg.EnablePunctuation
	param.EnableWords = true
	return param
}

func (a *Adapter) extraParams(cfg bootstrap.AliyunProviderConfig, req speechgateway.StartRequest) map[string]interface{} {
	extra := map[string]interface{}{
		"enable_voice_detection": cfg.EnableVoiceDetection,
		"session_id":             req.SessionID.String(),
	}
	if req.TenantID != uuid.Nil {
		extra["tenant_id"] = req.TenantID.String()
	}
	if req.UserID != uuid.Nil {
		extra["user_id"] = req.UserID.String()
	}
	for k, v := range req.Metadata {
		if strings.TrimSpace(k) == "" || v == "" {
			continue
		}
		extra[k] = v
	}
	return extra
}

func (a *Adapter) handleSentence(text string, param interface{}) {
	ctx := sessionParam(param)
	if ctx == nil {
		return
	}
	evt := toProviderEvent(ctx.provider, text)
	ctx.emit(evt)
}

func (a *Adapter) handleTaskFailed(text string, param interface{}) {
	ctx := sessionParam(param)
	if ctx == nil {
		return
	}
	ctx.emitError(text)
}

func (a *Adapter) handleClose(param interface{}) {
	ctx := sessionParam(param)
	if ctx != nil {
		ctx.close()
	}
}

type handlerBundle struct {
	taskFailed    func(string, interface{})
	started       func(string, interface{})
	sentenceBegin func(string, interface{})
	sentenceEnd   func(string, interface{})
	resultChanged func(string, interface{})
	completed     func(string, interface{})
	closed        func(interface{})
	param         interface{}
}

type speechFactory interface {
	New(*nls.ConnectionConfig, *nls.NlsLogger, handlerBundle) (*nls.SpeechTranscription, error)
}

func (a *Adapter) currentState() (bootstrap.AliyunProviderConfig, *TokenCache) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg, a.tokenCache
}

type defaultFactory struct{}

func (defaultFactory) New(cfg *nls.ConnectionConfig, logger *nls.NlsLogger, h handlerBundle) (*nls.SpeechTranscription, error) {
	return nls.NewSpeechTranscription(cfg, logger,
		h.taskFailed,
		h.started,
		h.sentenceBegin,
		h.sentenceEnd,
		h.resultChanged,
		h.completed,
		h.closed,
		h.param,
	)
}

func sessionParam(param interface{}) *sessionContext {
	ctx, _ := param.(*sessionContext)
	return ctx
}

type sessionContext struct {
	provider string
	events   chan speechgateway.ProviderEvent
	logger   *slog.Logger
	once     sync.Once
}

func newSessionContext(provider string, logger *slog.Logger) *sessionContext {
	if logger == nil {
		logger = slog.Default()
	}
	return &sessionContext{
		provider: provider,
		events:   make(chan speechgateway.ProviderEvent, 32),
		logger:   logger.With("provider", provider),
	}
}

func (c *sessionContext) emit(evt speechgateway.ProviderEvent) {
	evt.Provider = c.provider
	select {
	case c.events <- evt:
	default:
		c.logger.Warn("dropping provider event", "type", evt.Type, "sequence", evt.Sequence)
	}
}

func (c *sessionContext) emitError(text string) {
	err := fmt.Errorf("aliyun provider error: %s", strings.TrimSpace(text))
	c.emit(speechgateway.ProviderEvent{Type: speechgateway.EventTypeError, Err: err, Timestamp: time.Now()})
}

func (c *sessionContext) close() {
	c.once.Do(func() {
		close(c.events)
	})
}

func (c *sessionContext) channel() <-chan speechgateway.ProviderEvent {
	return c.events
}

type aliyunHandle struct {
	st     *nls.SpeechTranscription
	ctx    *sessionContext
	logger *slog.Logger
	closed atomic.Bool
}

func (h *aliyunHandle) SendAudio(_ context.Context, chunk speechgateway.AudioChunk) error {
	if len(chunk.Data) == 0 {
		return nil
	}
	return h.st.SendAudioData(chunk.Data)
}

func (h *aliyunHandle) Close(ctx context.Context) error {
	if h.closed.Swap(true) {
		return nil
	}
	ready, err := h.st.Stop()
	if err != nil {
		h.st.Shutdown()
		h.ctx.close()
		return err
	}
	wait := 5 * time.Second
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case ok := <-ready:
		if !ok {
			err = errors.New("aliyun stop rejected")
		}
	case <-ctx.Done():
		err = ctx.Err()
	case <-time.After(wait):
		err = errors.New("aliyun stop timeout")
	}
	h.st.Shutdown()
	h.ctx.close()
	return err
}

func (h *aliyunHandle) Events() <-chan speechgateway.ProviderEvent {
	return h.ctx.channel()
}

func toProviderEvent(provider string, raw string) speechgateway.ProviderEvent {
	evt := speechgateway.ProviderEvent{
		Provider:  provider,
		Type:      speechgateway.EventTypeInfo,
		Timestamp: time.Now(),
	}
	if raw != "" {
		evt.Raw = json.RawMessage([]byte(raw))
	}
	var payload aliEnvelope
	if err := json.Unmarshal([]byte(raw), &payload); err == nil {
		if payload.Payload.Result != nil {
			res := payload.Payload.Result
			evt.Text = res.Text
			evt.Sequence = res.Sequence
			evt.Confidence = float32(res.Confidence)
			evt.Metadata = map[string]any{
				"sentence_id": res.SentenceID,
				"begin_time":  res.BeginTime,
				"end_time":    res.EndTime,
				"status":      payload.Header.Status,
			}
		}
		name := strings.TrimSpace(payload.Header.Name)
		switch name {
		case nls.ST_RESULT_CHG_NAME:
			evt.Type = speechgateway.EventTypePartial
		case nls.ST_SENTENCE_END_NAME, nls.ST_COMPLETED_NAME:
			evt.Type = speechgateway.EventTypeFinal
			evt.Final = true
		default:
			evt.Type = speechgateway.EventTypeInfo
		}
	}
	return evt
}

type aliEnvelope struct {
	Header  aliHeader  `json:"header"`
	Payload aliPayload `json:"payload"`
}

type aliHeader struct {
	Name   string `json:"name"`
	Status int    `json:"status"`
}

type aliPayload struct {
	Result *aliResult `json:"result"`
}

type aliResult struct {
	Text       string  `json:"text"`
	Sequence   int64   `json:"sequence"`
	SentenceID int64   `json:"sentence_id"`
	Confidence float64 `json:"confidence"`
	BeginTime  int64   `json:"begin_time"`
	EndTime    int64   `json:"end_time"`
}
