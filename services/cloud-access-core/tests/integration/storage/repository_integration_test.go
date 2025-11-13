//go:build integration

package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/migrations"
	"github.com/hayhandsome/msbd/services/cloud-access-core/tests/integration/internal/testconf"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRepositoryIntegrationWithPostgres(t *testing.T) {
	ctx := context.Background()

	cfg := testconf.Load(t)
	require.NoError(t, testconf.EnsureDatabase(ctx, cfg.PostgresDSN))

	db, err := gorm.Open(postgres.Open(cfg.PostgresDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm open: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	defer sqlDB.Close()

	require.NoError(t, migrations.ApplyAll(ctx, sqlDB))

	repo := gormdb.NewRepository(db)
	require.NoError(t, gormdb.EnsureSeedData(ctx, repo))

	admin, err := repo.UserByEmail(ctx, "admin@cloud-access.local")
	if err != nil {
		t.Fatalf("admin lookup: %v", err)
	}

	device := &gormdb.Device{
		BaseModel:     gormdb.BaseModel{ID: uuid.New()},
		UserID:        admin.ID,
		TenantID:      admin.TenantID,
		Platform:      "macos",
		Fingerprint:   "fp-123",
		Status:        "approved",
		TrustScore:    80,
		LastSeen:      time.Now(),
		ConfigVersion: 2,
	}
	if err := repo.SaveDevice(ctx, device); err != nil {
		t.Fatalf("save device: %v", err)
	}
}
