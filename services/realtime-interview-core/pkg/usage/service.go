package usage

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/metrics"
)

// exporter abstracts persistence + publish handling for aggregated records.
type exporter interface {
	Export(ctx context.Context, records []Record) error
}

// Service aggregates speech + LLM usage and flushes windows via the exporter.
type Service struct {
	cfg      bootstrap.UsageExportConfig
	exporter exporter
	metrics  *metrics.Collector
	logger   *slog.Logger

	mu      sync.Mutex
	windows map[string]*Record

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewService builds a usage aggregation service. Returns nil if disabled.
func NewService(cfg bootstrap.UsageExportConfig, exporter exporter, metrics *metrics.Collector, logger *slog.Logger) *Service {
	if !cfg.Enabled || exporter == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		cfg:      cfg,
		exporter: exporter,
		metrics:  metrics,
		logger:   logger.With("component", "usage.service"),
		windows:  make(map[string]*Record),
	}
}

// RecordSpeech ingests a single speech sample.
func (s *Service) RecordSpeech(sample SpeechSample) {
	if s == nil {
		return
	}
	ts := sample.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	start, end := windowBounds(ts)
	seconds := secondsFromDuration(sample.Duration)
	if seconds == 0 && sample.Duration > 0 {
		seconds = 1
	}

	s.mu.Lock()
	record := s.ensureWindow(sample.SessionID, sample.TenantID, start, end)
	record.SpeechSeconds += seconds
	if sample.Errored {
		record.ErrorCount++
	}
	s.mu.Unlock()

	if sample.Duration > 0 {
		s.metrics.ObserveSpeech(sample.Provider, sample.Duration)
	} else if sample.ProviderLatency > 0 {
		s.metrics.ObserveSpeech(sample.Provider, sample.ProviderLatency)
	}
	if sample.Errored {
		s.metrics.RecordProviderError(sample.Provider)
	}
}

// RecordLLM tracks orchestrator token usage.
func (s *Service) RecordLLM(stat LLMStat) {
	if s == nil {
		return
	}
	ts := stat.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	start, end := windowBounds(ts)

	s.mu.Lock()
	record := s.ensureWindow(stat.SessionID, stat.TenantID, start, end)
	record.LLMTokens += max(stat.Tokens, 0)
	if stat.Degraded {
		record.ErrorCount++
	}
	s.mu.Unlock()

	s.metrics.ObserveLLMTokens(stat.Provider, stat.Tokens, stat.Degraded)
}

// Start begins the background flush loop.
func (s *Service) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go s.flushLoop()
	return nil
}

// Shutdown drains outstanding flushes.
func (s *Service) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return s.Flush(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Flush triggers a synchronous export of the current aggregation windows.
func (s *Service) Flush(ctx context.Context) error {
	if s == nil {
		return nil
	}
	records := s.snapshot()
	if len(records) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.exporter.Export(ctx, records); err != nil {
		s.metrics.RecordUsageFlush("error")
		// reinsert windows for retry
		s.restore(records)
		return err
	}
	s.metrics.RecordUsageFlush("ok")
	return nil
}

func (s *Service) flushLoop() {
	defer s.wg.Done()
	interval := s.cfg.FlushInterval()
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			_ = s.Flush(context.Background())
			return
		case <-ticker.C:
			if err := s.Flush(s.ctx); err != nil {
				s.logger.Warn("usage flush failed", "error", err)
			}
		}
	}
}

func (s *Service) ensureWindow(sessionID, tenantID uuid.UUID, start, end time.Time) *Record {
	if s.windows == nil {
		s.windows = make(map[string]*Record)
	}
	key := windowKey(sessionID, start)
	record, ok := s.windows[key]
	if !ok {
		record = &Record{SessionID: sessionID, TenantID: tenantID, WindowStart: start, WindowEnd: end}
		s.windows[key] = record
	}
	return record
}

func (s *Service) snapshot() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.windows) == 0 {
		return nil
	}
	records := make([]Record, 0, len(s.windows))
	for key, record := range s.windows {
		records = append(records, *record)
		delete(s.windows, key)
	}
	return records
}

func (s *Service) restore(records []Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.windows == nil {
		s.windows = make(map[string]*Record)
	}
	for _, record := range records {
		key := windowKey(record.SessionID, record.WindowStart)
		if existing, ok := s.windows[key]; ok {
			existing.SpeechSeconds += record.SpeechSeconds
			existing.LLMTokens += record.LLMTokens
			existing.ErrorCount += record.ErrorCount
			continue
		}
		recordCopy := record
		s.windows[key] = &recordCopy
	}
}

func windowKey(sessionID uuid.UUID, start time.Time) string {
	return fmt.Sprintf("%s:%d", sessionID.String(), start.Unix())
}

func windowBounds(ts time.Time) (time.Time, time.Time) {
	ts = ts.UTC()
	start := ts.Truncate(time.Minute)
	return start, start.Add(time.Minute)
}

func secondsFromDuration(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(math.Ceil(d.Seconds()))
}

func max(a, b int) int {
	if a >= b {
		return a
	}
	return b
}
