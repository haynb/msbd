//go:build integration

package iamtest

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/audit"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/crypto"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/iam"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/gormdb"
	"github.com/hayhandsome/msbd/services/cloud-access-core/pkg/storage/migrations"
	"github.com/hayhandsome/msbd/services/cloud-access-core/tests/integration/internal/testconf"
)

func TestIAMLoginFlowAgainstCloudStores(t *testing.T) {
	cfg := testconf.Load(t)
	ctx := context.Background()
	require.NoError(t, testconf.EnsureDatabase(ctx, cfg.PostgresDSN))

	db, err := gorm.Open(postgres.Open(cfg.PostgresDSN), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()

	require.NoError(t, migrations.ApplyAll(ctx, sqlDB))
	repo := gormdb.NewRepository(db)
	require.NoError(t, gormdb.EnsureSeedData(ctx, repo))

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	t.Cleanup(func() { _ = redisClient.Close() })
	require.NoError(t, redisClient.FlushDB(ctx).Err())

	signer, err := crypto.NewSigner(crypto.Options{Issuer: "integration-tests"})
	require.NoError(t, err)

	service := iam.NewService(
		repo,
		iam.NewRedisSessionStore(redisClient, "cloud-access-it"),
		signer,
		audit.NewDBRecorder(repo, slog.New(slog.NewTextHandler(io.Discard, nil))),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		iam.ServiceConfig{
			WebAudience:     "cloud-access-web",
			DeviceAudience:  "cloud-access-device",
			AccessTokenTTL:  time.Minute,
			DeviceTokenTTL:  time.Minute,
			RefreshTokenTTL: 10 * time.Minute,
		},
	)

	login, err := service.Login(ctx, iam.LoginRequest{
		Email:    "admin@cloud-access.local",
		Password: "ChangeMe!2024",
	})
	require.NoError(t, err)
	require.NotEmpty(t, login.AccessToken)
	require.NotEmpty(t, login.RefreshToken)

	refreshed, err := service.Refresh(ctx, login.RefreshToken)
	require.NoError(t, err)
	require.NotEqual(t, login.AccessToken, refreshed.AccessToken)

	require.NoError(t, service.Logout(ctx, refreshed.RefreshToken))
	_, err = service.Refresh(ctx, refreshed.RefreshToken)
	require.ErrorIs(t, err, iam.ErrInvalidCredentials)
}
