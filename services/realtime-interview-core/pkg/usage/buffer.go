package usage

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

	cloudEvents "github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
)

// BadgerBufferConfig describes the on-disk buffer settings.
type BadgerBufferConfig struct {
	Path          string
	FlushInterval time.Duration
	MaxBatch      int
	OnChange      func(int64)
}

// BadgerBuffer durably stores events when Redis is unavailable.
type BadgerBuffer struct {
	db        *badger.DB
	cfg       BadgerBufferConfig
	logger    *slog.Logger
	pending   atomic.Int64
	started   atomic.Bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	publisher cloudEvents.Publisher
}

// NewBadgerBuffer opens (or creates) the buffer.
func NewBadgerBuffer(cfg BadgerBufferConfig, logger *slog.Logger) (*BadgerBuffer, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.Path) == "" {
		cfg.Path = filepath.Join("data", "usage-buffer")
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.MaxBatch <= 0 {
		cfg.MaxBatch = 128
	}
	if err := os.MkdirAll(cfg.Path, 0o755); err != nil {
		return nil, fmt.Errorf("create usage buffer dir: %w", err)
	}
	opts := badger.DefaultOptions(cfg.Path)
	opts.Logger = badgerLogger{logger: logger}
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open usage buffer: %w", err)
	}
	buffer := &BadgerBuffer{db: db, cfg: cfg, logger: logger.With("component", "usage.buffer")}
	if err := buffer.recount(); err != nil {
		_ = db.Close()
		return nil, err
	}
	buffer.notify()
	return buffer, nil
}

// Start begins the flush loop targeting the provided publisher.
func (b *BadgerBuffer) Start(ctx context.Context, publisher cloudEvents.Publisher) error {
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

// Enqueue stores the event for later replay.
func (b *BadgerBuffer) Enqueue(event cloudEvents.Event) error {
	if b == nil || b.db == nil {
		return errors.New("usage buffer not configured")
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	key := fmt.Sprintf("%020d-%s", time.Now().UTC().UnixNano(), uuid.NewString())
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal usage event: %w", err)
	}
	if err := b.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), payload)
	}); err != nil {
		return fmt.Errorf("persist usage event: %w", err)
	}
	b.pending.Add(1)
	b.notify()
	return nil
}

// Pending returns the backlog size.
func (b *BadgerBuffer) Pending() int64 {
	if b == nil {
		return 0
	}
	return b.pending.Load()
}

// Flush drains the buffer once.
func (b *BadgerBuffer) Flush(ctx context.Context) error {
	if b == nil {
		return nil
	}
	return b.flushOnce(ctx)
}

// Close stops background workers and closes the DB.
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
		b.logger.Warn("initial usage buffer flush failed", "error", err)
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
				b.logger.Warn("usage buffer flush failed", "error", err)
			}
		}
	}
}

var errBufferEmpty = errors.New("usage buffer empty")

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
		var event cloudEvents.Event
		if err := json.Unmarshal(value, &event); err != nil {
			b.logger.Warn("discarding corrupted usage event", "error", err)
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
	if b.db == nil {
		return nil, nil, errBufferEmpty
	}
	var key, value []byte
	err := b.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.IteratorOptions{PrefetchValues: true})
		defer it.Close()
		it.Rewind()
		if !it.Valid() {
			return errBufferEmpty
		}
		item := it.Item()
		key = append([]byte{}, item.Key()...)
		return item.Value(func(val []byte) error {
			value = append([]byte{}, val...)
			return nil
		})
	})
	return key, value, err
}

func (b *BadgerBuffer) delete(key []byte) error {
	if b.db == nil {
		return nil
	}
	return b.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
}

func (b *BadgerBuffer) recount() error {
	if b.db == nil {
		return nil
	}
	count := int64(0)
	err := b.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.IteratorOptions{PrefetchValues: false})
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			count++
		}
		return nil
	})
	if err != nil {
		return err
	}
	b.pending.Store(count)
	return nil
}

func (b *BadgerBuffer) notify() {
	if b == nil || b.cfg.OnChange == nil {
		return
	}
	b.cfg.OnChange(b.pending.Load())
}

type badgerLogger struct {
	logger *slog.Logger
}

func (l badgerLogger) Errorf(format string, args ...interface{}) {
	if l.logger != nil {
		l.logger.Error(fmt.Sprintf(format, args...))
	}
}

func (l badgerLogger) Warningf(format string, args ...interface{}) {
	if l.logger != nil {
		l.logger.Warn(fmt.Sprintf(format, args...))
	}
}

func (l badgerLogger) Infof(string, ...interface{})  {}
func (l badgerLogger) Debugf(string, ...interface{}) {}
