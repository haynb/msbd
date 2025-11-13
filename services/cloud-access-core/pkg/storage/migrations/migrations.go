package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	initSchemaOnce sync.Once
	initSchemaData string
	initSchemaErr  error
)

func loadInitSchema() (string, error) {
	initSchemaOnce.Do(func() {
		_, currentFile, _, ok := runtime.Caller(0)
		if !ok {
			initSchemaErr = fmt.Errorf("unable to determine caller path for migrations")
			return
		}
		projectRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
		path := filepath.Join(projectRoot, "db", "migrations", "000001_init_cloud_access.sql")
		data, err := os.ReadFile(path)
		if err != nil {
			initSchemaErr = fmt.Errorf("read migration %q: %w", path, err)
			return
		}
		initSchemaData = string(data)
	})
	return initSchemaData, initSchemaErr
}

// ApplyAll executes the SQL migrations against the provided DB connection.
func ApplyAll(ctx context.Context, db *sql.DB) error {
	sql, err := loadInitSchema()
	if err != nil {
		return err
	}
	up := strings.TrimSpace(upSection(sql))
	if up == "" {
		return fmt.Errorf("init schema missing goose up section")
	}
	if _, err := db.ExecContext(ctx, up); err != nil {
		return fmt.Errorf("apply init schema: %w", err)
	}
	return nil
}

func upSection(contents string) string {
	marker := "\n-- +goose Down"
	if idx := strings.Index(contents, marker); idx != -1 {
		return contents[:idx]
	}
	return contents
}
