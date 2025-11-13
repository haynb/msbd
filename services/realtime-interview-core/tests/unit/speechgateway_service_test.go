package unit

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/speechgateway"
)

func TestSpeechGateway_OpenAndStream(t *testing.T) {
	svc, provider, cleanup := newTestGateway(t, bootstrap.SpeechGatewayConfig{
		Enabled:                   true,
		MaxChunkBytes:             1024,
		SessionIdleTimeoutSeconds: 5,
		BufferPrefix:              "test-chunks",
		BufferTTLSeconds:          30,
	})
	defer cleanup()

	session, err := svc.OpenStream(context.Background(), speechgateway.StartRequest{
		SessionID:  uuid.New(),
		TenantID:   uuid.New(),
		SampleRate: 16000,
		Provider:   "fake",
	})
	require.NoError(t, err)
	require.NotNil(t, session)
	require.Equal(t, 16000, session.Ack().SampleRate)

	reqCtx := context.Background()
	require.NoError(t, session.SendChunk(reqCtx, speechgateway.AudioChunk{Sequence: 1, Data: []byte("pcm"), ReceivedAt: time.Now()}))

	select {
	case evt := <-session.Events():
		require.Equal(t, int64(1), evt.Sequence)
		require.Equal(t, "chunk-1", evt.Text)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for provider event")
	}

	err = session.SendChunk(reqCtx, speechgateway.AudioChunk{Sequence: 1, Data: []byte("next"), ReceivedAt: time.Now().Add(time.Millisecond)})
	require.ErrorIs(t, err, speechgateway.ErrOutOfOrder)
	require.NoError(t, session.Close(context.Background()))
	require.Equal(t, int64(1), provider.lastSeq.Load())
}

func TestSpeechGateway_BuffersOnFailure(t *testing.T) {
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	t.Cleanup(func() {
		client.Close()
		mini.Close()
	})

	svc := speechgateway.NewService(bootstrap.SpeechGatewayConfig{
		Enabled:                   true,
		MaxChunkBytes:             1024,
		SessionIdleTimeoutSeconds: 5,
		BufferPrefix:              "gateway-buffer",
		BufferTTLSeconds:          60,
	}, "fake", client, nil, nil, nil)
	provider := &failingProvider{}
	svc.RegisterProvider("fake", provider)

	session, err := svc.OpenStream(context.Background(), speechgateway.StartRequest{SessionID: uuid.New(), Provider: "fake"})
	require.NoError(t, err)
	require.Error(t, session.SendChunk(context.Background(), speechgateway.AudioChunk{Sequence: 1, Data: []byte("oops"), ReceivedAt: time.Now()}))

	key := "gateway-buffer:" + session.ID().String()
	vals, err := client.HGetAll(context.Background(), key).Result()
	require.NoError(t, err)
	require.Contains(t, vals, "1")
}

func newTestGateway(t *testing.T, cfg bootstrap.SpeechGatewayConfig) (*speechgateway.Service, *fakeProvider, func()) {
	t.Helper()
	mini := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mini.Addr()})
	svc := speechgateway.NewService(cfg, "fake", client, nil, nil, nil)
	provider := newFakeProvider()
	svc.RegisterProvider("fake", provider)
	cleanup := func() {
		client.Close()
		mini.Close()
	}
	return svc, provider, cleanup
}

type fakeProvider struct {
	events  chan speechgateway.ProviderEvent
	lastSeq atomic.Int64
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{events: make(chan speechgateway.ProviderEvent, 8)}
}

func (f *fakeProvider) Start(ctx context.Context, params speechgateway.StartRequest) (speechgateway.StreamHandle, error) {
	return &fakeHandle{events: f.events, seq: &f.lastSeq}, nil
}

type fakeHandle struct {
	events chan speechgateway.ProviderEvent
	seq    *atomic.Int64
}

func (h *fakeHandle) SendAudio(_ context.Context, chunk speechgateway.AudioChunk) error {
	h.seq.Store(chunk.Sequence)
	h.events <- speechgateway.ProviderEvent{Sequence: chunk.Sequence, Text: fmt.Sprintf("chunk-%d", chunk.Sequence), Provider: "fake"}
	return nil
}

func (h *fakeHandle) Close(context.Context) error {
	close(h.events)
	return nil
}

func (h *fakeHandle) Events() <-chan speechgateway.ProviderEvent {
	return h.events
}

type failingProvider struct{}

func (f *failingProvider) Start(ctx context.Context, params speechgateway.StartRequest) (speechgateway.StreamHandle, error) {
	return &failHandle{}, nil
}

type failHandle struct{}

func (f *failHandle) SendAudio(context.Context, speechgateway.AudioChunk) error {
	return errors.New("inject failure")
}

func (f *failHandle) Close(context.Context) error { return nil }

func (f *failHandle) Events() <-chan speechgateway.ProviderEvent {
	ch := make(chan speechgateway.ProviderEvent)
	close(ch)
	return ch
}
