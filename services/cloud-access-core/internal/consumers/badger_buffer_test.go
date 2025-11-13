package consumers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/stretchr/testify/require"
)

func TestBadgerBufferEnqueueAndFlush(t *testing.T) {
	dir := t.TempDir()
	buf, err := NewBadgerBuffer(BadgerBufferConfig{Path: dir, FlushInterval: 10 * time.Millisecond}, newTestLogger())
	require.NoError(t, err)
	defer buf.Close()

	publisher := &testPublisher{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, buf.Start(ctx, publisher))

	evt := events.Event{Topic: "audit", Payload: map[string]any{"action": "login"}, Timestamp: time.Now()}
	require.NoError(t, buf.Enqueue(evt))

	require.Eventually(t, func() bool {
		return buf.Pending() == 0 && publisher.count() == 1
	}, time.Second, 20*time.Millisecond)
}

func TestBadgerBufferFlushHandlesEmpty(t *testing.T) {
	dir := t.TempDir()
	buf, err := NewBadgerBuffer(BadgerBufferConfig{Path: dir}, newTestLogger())
	require.NoError(t, err)
	defer buf.Close()

	publisher := &testPublisher{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, buf.Start(ctx, publisher))

	err = buf.Flush(ctx)
	if err != nil {
		require.True(t, errors.Is(err, errBufferEmpty))
	}
}

type testPublisher struct {
	events []events.Event
}

func (p *testPublisher) Publish(ctx context.Context, event events.Event) error {
	p.events = append(p.events, event)
	return nil
}

func (p *testPublisher) count() int {
	return len(p.events)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}
