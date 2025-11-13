package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/audit"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"github.com/stretchr/testify/require"
)

func TestAuditRecorderPublishesAndPersists(t *testing.T) {
	writer := &stubWriter{}
	publisher := &stubPublisher{}
	buffer := &stubBuffer{}
	recorder := NewAuditRecorder(writer, publisher, buffer, nil, nil, AuditRecorderConfig{})

	entry := audit.Entry{TenantID: uuid.New(), ActorID: uuid.New(), Action: "auth.login", Result: "success", Latency: 25 * time.Millisecond}
	recorder.Record(context.Background(), entry)

	require.Len(t, writer.entries, 1)
	require.Len(t, publisher.events, 1)
	require.Empty(t, buffer.events)
}

func TestAuditRecorderBuffersOnPublishFailure(t *testing.T) {
	writer := &stubWriter{}
	publisher := &stubPublisher{err: errors.New("redis down")}
	buffer := &stubBuffer{}
	recorder := NewAuditRecorder(writer, publisher, buffer, nil, nil, AuditRecorderConfig{})

	recorder.Record(context.Background(), audit.Entry{TenantID: uuid.New(), ActorID: uuid.New(), Action: "policy.update", Result: "success"})

	require.Len(t, writer.entries, 1)
	require.Len(t, buffer.events, 1)
}

func TestAuditRecorderStopsOnDatabaseFailure(t *testing.T) {
	writer := &stubWriter{err: errors.New("db error")}
	publisher := &stubPublisher{}
	buffer := &stubBuffer{}
	recorder := NewAuditRecorder(writer, publisher, buffer, nil, nil, AuditRecorderConfig{})

	recorder.Record(context.Background(), audit.Entry{TenantID: uuid.New(), ActorID: uuid.New(), Action: "config.save", Result: "failure"})

	require.Empty(t, publisher.events)
	require.Empty(t, buffer.events)
}

type stubWriter struct {
	entries []*gormdb.AuditLog
	err     error
}

func (s *stubWriter) RecordAuditLog(ctx context.Context, logEntry *gormdb.AuditLog) error {
	if s.err != nil {
		return s.err
	}
	s.entries = append(s.entries, logEntry)
	return nil
}

type stubPublisher struct {
	events []events.Event
	err    error
}

func (s *stubPublisher) Publish(ctx context.Context, event events.Event) error {
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, event)
	return nil
}

type stubBuffer struct {
	events []events.Event
	err    error
}

func (s *stubBuffer) Enqueue(event events.Event) error {
	if s.err != nil {
		return s.err
	}
	s.events = append(s.events, event)
	return nil
}

func (s *stubBuffer) Pending() int64 {
	return int64(len(s.events))
}
