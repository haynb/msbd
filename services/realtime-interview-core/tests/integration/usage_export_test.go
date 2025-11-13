//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	cloudEvents "github.com/hayhandsome/msbd/services/cloud-access-core/pkg/events"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/bootstrap"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/sessions"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/storage/gormdb"
	"github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/storage/migrations"
	usagepkg "github.com/hayhandsome/msbd/services/realtime-interview-core/pkg/usage"
)

func TestUsageExporterBuffersOnPublishFailure(t *testing.T) {
	ctx := context.Background()
	db, cleanup := setupUsagePostgres(t, ctx)
	t.Cleanup(cleanup)

	repo := sessions.NewRepository(db)
	session := &gormdb.Session{TenantID: uuid.New(), UserID: uuid.New(), Mode: "interview"}
	require.NoError(t, repo.CreateSession(ctx, session))

	publisher := &flakyPublisher{failures: 1}
	bufferDir := filepath.Join(t.TempDir(), "usage-buffer")
	buffer, err := usagepkg.NewBadgerBuffer(usagepkg.BadgerBufferConfig{Path: bufferDir, FlushInterval: 50 * time.Millisecond}, slog.Default())
	require.NoError(t, err)

	exporter := usagepkg.NewExporter(repo, publisher, buffer, "integration:usage", nil, slog.Default())
	svc := usagepkg.NewService(bootstrap.UsageExportConfig{Enabled: true, FlushIntervalSeconds: 1}, exporter, nil, slog.Default())
	require.NotNil(t, svc)

	svc.RecordSpeech(usagepkg.SpeechSample{SessionID: session.ID, TenantID: session.TenantID, Duration: 2 * time.Second, Timestamp: time.Now().UTC()})
	require.NoError(t, svc.Flush(ctx))

	require.Eventually(t, func() bool {
		var count int64
		if err := db.WithContext(ctx).Model(&gormdb.SessionUsage{}).Where("session_id = ?", session.ID).Count(&count).Error; err != nil {
			return false
		}
		return count == 1
	}, 5*time.Second, 100*time.Millisecond)

	require.Greater(t, buffer.Pending(), int64(0))
	publisher.failures = 0
	require.NoError(t, exporter.FlushBuffer(ctx))
	require.Eventually(t, func() bool { return publisher.published == 1 }, 3*time.Second, 100*time.Millisecond)
}

func setupUsagePostgres(t *testing.T, ctx context.Context) (*gorm.DB, func()) {
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
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: req, Started: true})
	require.NoError(t, err)

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)
	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/rtcore?sslmode=disable", host, port.Port())
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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

type flakyPublisher struct {
	failures  int
	published int
}

func (f *flakyPublisher) Publish(ctx context.Context, event cloudEvents.Event) error {
	if f.failures > 0 {
		f.failures--
		return errors.New("publisher unavailable")
	}
	f.published++
	return nil
}
