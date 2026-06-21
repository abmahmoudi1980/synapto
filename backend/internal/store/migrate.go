package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationDir is the directory inside the embed FS that holds migrations.
const migrationDir = "migrations"

// Migrate applies all pending SQL migrations to the given database, in
// lexical order by filename. It records applied versions in the
// schema_migrations table. A failed migration aborts with its error and
// leaves the database untouched (each migration runs in its own tx).
func Migrate(ctx context.Context, db *sql.DB, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	files, err := migrationFS.ReadDir(migrationDir)
	if err != nil {
		return fmt.Errorf("store: read migrations dir: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".sql") {
			continue
		}
		version, ok := versionFromName(f.Name())
		if !ok {
			log.Warn("store: skipping migration with unparsable name", "file", f.Name())
			continue
		}
		if _, already := applied[version]; already {
			continue
		}
		sqlBytes, err := migrationFS.ReadFile(migrationDir + "/" + f.Name())
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", f.Name(), err)
		}
		log.Info("store: applying migration", "file", f.Name(), "version", version)
		if err := applyOne(ctx, db, version, string(sqlBytes)); err != nil {
			return fmt.Errorf("store: migration %s: %w", f.Name(), err)
		}
	}
	return nil
}

// appliedVersions returns the set of migration versions already applied.
func appliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("store: query schema_migrations: %w", err)
	}
	defer rows.Close()
	out := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("store: scan schema_migrations: %w", err)
		}
		out[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: rows schema_migrations: %w", err)
	}
	return out, nil
}

// versionFromName parses "0001_init.sql" → 1. Returns false on failure.
func versionFromName(name string) (int, bool) {
	var v int
	for _, r := range name {
		if r < '0' || r > '9' {
			break
		}
		v = v*10 + int(r-'0')
	}
	if v == 0 {
		return 0, false
	}
	return v, true
}

// applyOne runs one migration in a transaction and records its version.
func applyOne(ctx context.Context, db *sql.DB, version int, sqlText string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // commit is the success path

	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))", version); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	return tx.Commit()
}
