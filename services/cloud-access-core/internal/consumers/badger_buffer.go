package consumers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
)

// BadgerBufferConfig controls the on-disk buffer behaviour.
type BadgerBufferConfig struct {
	Path          string
	FlushInterval time.Duration
	MaxBatch      int
	OnChange      func(int64)
}

// BadgerBuffer durable-queues events and retries publishing when the backend is unavailable.
type BadgerBuffer struct {
	db        *badger.DB
	cfg       BadgerBufferConfig
	logger    *slog.Logger
	pending   atomic.Int64
	started   atomic.Bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	publisher events.Publisher
}

var errBufferEmpty = errors.New("buffer empty")

// NewBadgerBuffer opens (or creates) the on-disk buffer.
func NewBadgerBuffer(cfg BadgerBufferConfig, logger *slog.Logger) (*BadgerBuffer, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.Path) == "" {
		cfg.Path = filepath.Join("data", "telemetry-buffer")
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.MaxBatch <= 0 {
		cfg.MaxBatch = 128
	}
	if err := os.MkdirAll(cfg.Path, 0o755); err != nil {
		return nil, fmt.Errorf("create buffer dir: %w", err)
	}
	opts := badger.DefaultOptions(cfg.Path)
	opts.Logger = newBadgerLogger(logger)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open badger buffer: %w", err)
	}
	buffer := &BadgerBuffer{db: db, cfg: cfg, logger: logger.With("component", "badger_buffer")}
	if err := buffer.recountPending(); err != nil {
		_ = db.Close()
		return nil, err
	}
	buffer.notify()
	return buffer, nil
}

// Start begins the flush loop.
func (b *BadgerBuffer) Start(ctx context.Context, publisher events.Publisher) error {
	if b == nil || b.db == nil {
		return nil
	}
	if publisher == nil {
		return errors.New("publisher required")
	}
	if b.started.Load() {
		return nil
	}
	b.publisher = publisher
	b.ctx, b.cancel = context.WithCancel(ctx)
	b.started.Store(true)
	b.wg.Add(1)
	go b.loop()
	return nil
}

// Enqueue persists the event for later replay.
func (b *BadgerBuffer) Enqueue(event events.Event) error {
	if b == nil || b.db == nil {
		return errors.New("buffer not configured")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	key := fmt.Sprintf("%020d-%s", time.Now().UTC().UnixNano(), uuid.NewString())
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal buffered event: %w", err)
	}
	if err := b.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), payload)
	}); err != nil {
		return fmt.Errorf("persist buffered event: %w", err)
	}
	b.pending.Add(1)
	b.notify()
	return nil
}

// Pending returns the current backlog size.
func (b *BadgerBuffer) Pending() int64 {
	if b == nil {
		return 0
	}
	return b.pending.Load()
}

// Flush attempts to drain buffered events once.
func (b *BadgerBuffer) Flush(ctx context.Context) error {
	if b == nil {
		return nil
	}
	return b.flushOnce(ctx)
}

// Close stops background workers and closes the underlying DB.
func (b *BadgerBuffer) Close() error {
	if b == nil {
		return nil
	}
	if b.cancel != nil {
		b.cancel()
	}
	b.wg.Wait()
	if b.db != nil {
		return b.db.Close()
	}
	return nil
}

func (b *BadgerBuffer) loop() {
	defer b.wg.Done()
	if err := b.flushOnce(b.ctx); err != nil && !errors.Is(err, errBufferEmpty) {
		b.logger.Warn("initial buffer flush failed", "error", err)
	}
	ticker := time.NewTicker(b.cfg.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-b.ctx.Done():
			_ = b.flushOnce(context.Background())
			return
		case <-ticker.C:
			if err := b.flushOnce(b.ctx); err != nil && !errors.Is(err, errBufferEmpty) {
				b.logger.Warn("buffer flush failed", "error", err)
			}
		}
	}
}

func (b *BadgerBuffer) flushOnce(ctx context.Context) error {
	if b.publisher == nil {
		return nil
	}
	processed := 0
	for processed < b.cfg.MaxBatch {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		key, value, err := b.peek()
		if err != nil {
			if errors.Is(err, errBufferEmpty) {
				return errBufferEmpty
			}
			return err
		}
		var event events.Event
		if err := json.Unmarshal(value, &event); err != nil {
			b.logger.Warn("discarding corrupt buffered event", "error", err)
			_ = b.delete(key)
			b.pending.Add(-1)
			b.notify()
			continue
		}
		if err := b.publisher.Publish(ctx, event); err != nil {
			return err
		}
		if err := b.delete(key); err != nil {
			return err
		}
		b.pending.Add(-1)
		b.notify()
		processed++
	}
	return nil
}

func (b *BadgerBuffer) peek() ([]byte, []byte, error) {
	var keyCopy, valueCopy []byte
	err := b.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		it.Rewind()
		if !it.Valid() {
			return errBufferEmpty
		}
		item := it.Item()
		keyCopy = append([]byte(nil), item.Key()...)
		val, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		valueCopy = val
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return keyCopy, valueCopy, nil
}

func (b *BadgerBuffer) delete(key []byte) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

func (b *BadgerBuffer) recountPending() error {
	var count int64
	err := b.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			count++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("count buffered events: %w", err)
	}
	b.pending.Store(count)
	return nil
}

func (b *BadgerBuffer) notify() {
	if b.cfg.OnChange != nil {
		b.cfg.OnChange(b.pending.Load())
	}
}

type badgerLogger struct {
	logger *slog.Logger
}

func newBadgerLogger(logger *slog.Logger) badger.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return &badgerLogger{logger: logger.With("component", "badger")}
}

func (l *badgerLogger) Errorf(format string, args ...any) {
	l.logger.Error(fmt.Sprintf(format, args...))
}

func (l *badgerLogger) Warningf(format string, args ...any) {
	l.logger.Warn(fmt.Sprintf(format, args...))
}

func (l *badgerLogger) Infof(format string, args ...any) {
	l.logger.Info(fmt.Sprintf(format, args...))
}

func (l *badgerLogger) Debugf(format string, args ...any) {
	l.logger.Debug(fmt.Sprintf(format, args...))
}
