package testconf

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

// Config captures runtime endpoints for integration tests.
type Config struct {
	PostgresDSN   string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
}

// Load loads configuration from environment variables or skips the test if missing.
func Load(t *testing.T) Config {
	t.Helper()
	pg := os.Getenv("CLOUD_ACCESS_TEST_POSTGRES_DSN")
	if pg == "" {
		t.Skip("CLOUD_ACCESS_TEST_POSTGRES_DSN not set")
	}
	redisAddr := os.Getenv("CLOUD_ACCESS_TEST_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("CLOUD_ACCESS_TEST_REDIS_ADDR not set")
	}
	dbNum := 0
	if raw := os.Getenv("CLOUD_ACCESS_TEST_REDIS_DB"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			dbNum = parsed
		}
	}
	return Config{
		PostgresDSN:   pg,
		RedisAddr:     redisAddr,
		RedisPassword: os.Getenv("CLOUD_ACCESS_TEST_REDIS_PASSWORD"),
		RedisDB:       dbNum,
	}
}

// EnsureDatabase creates the database referenced by the DSN if it does not already exist.
func EnsureDatabase(ctx context.Context, dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return err
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return fmt.Errorf("dsn missing database name: %s", dsn)
	}
	admin := *u
	admin.Path = "/postgres"
	adminDSN := admin.String()
	conn, err := sql.Open("postgres", adminDSN)
	if err != nil {
		return err
	}
	defer conn.Close()
	safeName := strings.ReplaceAll(dbName, `"`, "")
	createStmt := fmt.Sprintf(`CREATE DATABASE "%s"`, safeName)
	if _, err := conn.ExecContext(ctx, createStmt); err != nil && !strings.Contains(err.Error(), "already exists") {
		return err
	}
	return nil
}
