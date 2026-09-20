package storage

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Version verifies the SQLite connection without creating application tables.
// A file path can be supplied to exercise an on-disk database.
func Version(ctx context.Context, path string) (string, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return "", fmt.Errorf("open SQLite: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var version string
	if err := db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err != nil {
		return "", fmt.Errorf("query SQLite: %w", err)
	}
	return version, nil
}
