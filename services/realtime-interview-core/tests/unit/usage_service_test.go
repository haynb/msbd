package unit

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/usage"
)

func TestUsageServiceAggregatesAndFlushes(t *testing.T) {
	exporter := &stubExporter{}
	svc := usage.NewService(bootstrap.UsageExportConfig{Enabled: true, FlushIntervalSeconds: 1}, exporter, nil, slog.Default())
	require.NotNil(t, svc)

	sessionID := uuid.New()
	tenantID := uuid.New()
	ts := time.Date(2024, 1, 1, 10, 0, 10, 0, time.UTC)

	svc.RecordSpeech(usage.SpeechSample{SessionID: sessionID, TenantID: tenantID, Duration: 1500 * time.Millisecond, Timestamp: ts})
	svc.RecordLLM(usage.LLMStat{SessionID: sessionID, TenantID: tenantID, Tokens: 120, Timestamp: ts.Add(5 * time.Second)})

	require.NoError(t, svc.Flush(context.Background()))
	require.Len(t, exporter.records, 1)
	record := exporter.records[0]
	require.Equal(t, 2, record.SpeechSeconds)
	require.Equal(t, 120, record.LLMTokens)
	require.Equal(t, 0, record.ErrorCount)

	exporter.records = nil
	exporter.err = errors.New("boom")
	svc.RecordSpeech(usage.SpeechSample{SessionID: sessionID, TenantID: tenantID, Duration: time.Second, Timestamp: ts.Add(time.Minute)})
	err := svc.Flush(context.Background())
	require.Error(t, err)
	require.Len(t, exporter.records, 0)

	exporter.err = nil
	require.NoError(t, svc.Flush(context.Background()))
	require.Len(t, exporter.records, 1)
}

type stubExporter struct {
	records []usage.Record
	err     error
}

func (s *stubExporter) Export(ctx context.Context, records []usage.Record) error {
	if s.err != nil {
		return s.err
	}
	s.records = append(s.records, records...)
	return nil
}
