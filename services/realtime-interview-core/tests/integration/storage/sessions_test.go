//go:build integration

package storage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/sessions"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/storage/gormdb"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/storage/migrations"
)

func TestSessionLifecycleRepository(t *testing.T) {
	ctx := context.Background()
	db, cleanup := setupPostgres(t, ctx)
	defer cleanup()

	repo := sessions.NewRepository(db)

	tenantID := uuid.New()
	userID := uuid.New()
	deviceID := uuid.New()

	session := &gormdb.Session{
		TenantID:   tenantID,
		UserID:     userID,
		DeviceID:   &deviceID,
		Mode:       "interview",
		Metadata:   datatypes.JSONMap{"intent": "mock"},
		RequestKey: "req-123",
	}
	require.NoError(t, repo.CreateSession(ctx, session))

	fetched, err := repo.SessionByID(ctx, session.ID)
	require.NoError(t, err)
	require.Equal(t, "active", fetched.State)

	pausedAt := time.Now().UTC()
	paused, err := repo.TransitionSession(ctx, session.ID, sessions.TransitionOptions{
		ExpectedState: "active",
		NextState:     "paused",
		HeartbeatAt:   pausedAt,
		MetadataPatch: datatypes.JSONMap{"pause_reason": "network"},
	})
	require.NoError(t, err)
	require.Equal(t, "paused", paused.State)
	require.WithinDuration(t, pausedAt, paused.LastHeartbeat, time.Second)
	require.Equal(t, "network", paused.Metadata["pause_reason"])

	_, err = repo.TransitionSession(ctx, session.ID, sessions.TransitionOptions{
		ExpectedState: "active",
		NextState:     "ended",
	})
	require.ErrorIs(t, err, sessions.ErrSessionStateConflict)

	endTime := time.Now().UTC()
	ended, err := repo.TransitionSession(ctx, session.ID, sessions.TransitionOptions{
		ExpectedState: "paused",
		NextState:     "ended",
		EndedAt:       &endTime,
	})
	require.NoError(t, err)
	require.Equal(t, "ended", ended.State)
	require.NotNil(t, ended.EndedAt)

	participant := &gormdb.SessionParticipant{
		SessionID: session.ID,
		UserID:    uuid.New(),
		Role:      "interviewer",
	}
	require.NoError(t, repo.AddParticipant(ctx, participant))
	require.NoError(t, repo.AddParticipant(ctx, participant)) // idempotent insert

	require.NoError(t, repo.MarkParticipantLeft(ctx, participant.SessionID, participant.UserID, participant.Role, time.Now().UTC()))

	err = repo.MarkParticipantLeft(ctx, participant.SessionID, uuid.New(), participant.Role, time.Now().UTC())
	require.ErrorIs(t, err, sessions.ErrParticipantNotFound)

	segment := &gormdb.SessionSegment{
		SessionID: session.ID,
		Transcript: datatypes.JSONMap{
			"text": "hello",
		},
		Confidence:            0.92,
		ProviderLatencyMillis: 120,
	}
	require.NoError(t, repo.AppendSegment(ctx, segment))
	require.Equal(t, 1, segment.Sequence)

	segment2 := &gormdb.SessionSegment{SessionID: session.ID}
	require.NoError(t, repo.AppendSegment(ctx, segment2))
	require.Equal(t, 2, segment2.Sequence)

	var storedSegments []gormdb.SessionSegment
	require.NoError(t, db.WithContext(ctx).Order("sequence").Find(&storedSegments, "session_id = ?", session.ID).Error)
	require.Len(t, storedSegments, 2)

	windowStart := time.Now().UTC().Truncate(time.Minute)
	windowEnd := windowStart.Add(time.Minute)
	usage := &gormdb.SessionUsage{
		SessionID:     session.ID,
		TenantID:      tenantID,
		SpeechSeconds: 30,
		LLMTokens:     200,
		ErrorCount:    1,
		WindowStart:   windowStart,
		WindowEnd:     windowEnd,
	}
	require.NoError(t, repo.UpsertUsageWindow(ctx, usage))

	require.NoError(t, repo.UpsertUsageWindow(ctx, &gormdb.SessionUsage{
		SessionID:     session.ID,
		TenantID:      tenantID,
		SpeechSeconds: 10,
		LLMTokens:     40,
		ErrorCount:    2,
		WindowStart:   windowStart,
		WindowEnd:     windowEnd,
	}))

	var aggregate gormdb.SessionUsage
	require.NoError(t, db.WithContext(ctx).
		First(&aggregate, "session_id = ? AND window_start = ?", session.ID, windowStart).Error)
	require.Equal(t, 40, aggregate.SpeechSeconds)
	require.Equal(t, 240, aggregate.LLMTokens)
	require.Equal(t, 3, aggregate.ErrorCount)

	sessionIDCopy := session.ID
	metric := &gormdb.SpeechProviderMetric{
		SessionID: &sessionIDCopy,
		Provider:  "aliyun",
		EventType: "partial",
		Value:     datatypes.JSONMap{"latency_ms": 110},
	}
	require.NoError(t, repo.RecordProviderMetric(ctx, metric))

	var metricCount int64
	require.NoError(t, db.WithContext(ctx).Model(&gormdb.SpeechProviderMetric{}).Count(&metricCount).Error)
	require.EqualValues(t, 1, metricCount)
}

func setupPostgres(t *testing.T, ctx context.Context) (*gorm.DB, func()) {
	t.Helper()
	req := testcontainers.ContainerRequest{
		Image:        "docker.io/postgres:16",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_USER":     "postgres",
			"POSTGRES_DB":       "rtcore",
		},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/rtcore?sslmode=disable", host, port.Port())
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.PingContext(ctx))

	require.NoError(t, migrations.ApplyAll(ctx, sqlDB))

	cleanup := func() {
		_ = sqlDB.Close()
		_ = container.Terminate(context.Background())
	}

	return db, cleanup
}
