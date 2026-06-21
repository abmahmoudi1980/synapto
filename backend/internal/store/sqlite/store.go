// Package sqlite implements store.* interfaces over a single SQLite file.
package sqlite

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // pure-Go SQLite driver

	"github.com/synapto/assistant/internal/store"
)

// Store is the concrete SQLite implementation of all repository interfaces.
// A single Store owns a single database file; writes are serialized with a
// mutex to keep WAL mode simple.
type Store struct {
	db  *sqlx.DB
	log *slog.Logger
	mu  sync.Mutex
}

// Open opens (or creates) the SQLite database at path, enables WAL mode,
// applies pending migrations, and seeds default settings + categories.
// The returned Store implements ChannelRepo, CategoryRepo, SettingsRepo,
// CycleRepo, DigestRepo, CursorRepo, and HealthRepo.
func Open(ctx context.Context, path string, log *slog.Logger) (*Store, error) {
	if log == nil {
		log = slog.Default()
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	db, err := sqlx.Connect("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // serialize writers at the pool level too
	s := &Store{db: db, log: log}
	if err := store.Migrate(ctx, db.DB, log); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.seedDefaults(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying *sqlx.DB for advanced callers (tests).
func (s *Store) DB() *sqlx.DB { return s.db }

// seedDefaults inserts the singleton settings row and the default categories
// if they do not yet exist. Idempotent.
func (s *Store) seedDefaults(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO settings (
		id, digest_interval_seconds, telegram_bot_token_ref, telegram_subscriber_chat,
		ai_provider, ai_model, ai_api_key_ref, ai_base_url, uncategorized_label, updated_at
	) VALUES ('singleton', 600, 'env:TELEGRAM_BOT_TOKEN', 0,
		'fake', 'gpt-4o-mini', 'env:AI_API_KEY', 'https://api.openai.com/v1', 'Uncategorized', ?)`,
		time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}

	defaults := []string{"Politics", "Technology", "Business", "Sports", "World", "Other"}
	for i, name := range defaults {
		_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO categories
			(id, name, ordering, is_default, created_at, updated_at)
			VALUES (?, ?, ?, 1, ?, ?)`,
			uuidLike(name), name, i, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return err
		}
	}
	return nil
}

// Begin starts a write transaction guarded by the store mutex. Callers must
// Commit or Rollback to release the mutex.
func (s *Store) Begin(ctx context.Context) (*Tx, error) {
	s.mu.Lock()
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	return &Tx{Tx: tx, store: s}, nil
}

// Tx wraps a sqlx.Tx and releases the store mutex on commit/rollback.
type Tx struct {
	*sqlx.Tx
	store *Store
}

// Commit commits the transaction and releases the store mutex.
func (t *Tx) Commit() error {
	err := t.Tx.Commit()
	t.store.mu.Unlock()
	return err
}

// Rollback rolls the transaction back and releases the store mutex.
func (t *Tx) Rollback() error {
	err := t.Tx.Rollback()
	t.store.mu.Unlock()
	return err
}

// uuidLike produces a stable-ish string for seed rows where we don't want
// to import the uuid package just for defaults. Tests overwrite these.
func uuidLike(seed string) string {
	now := time.Now().UTC().Format("20060102")
	return "seed-" + now + "-" + seed
}

// Common helpers ---------------------------------------------------------------

func nowISO() string { return time.Now().UTC().Format(time.RFC3339) }

func parseTime(s sql.NullString) time.Time {
	if !s.Valid || s.String == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseTimeStr(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func nullTime(t time.Time) sql.NullString {
	if t.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{Valid: true, String: t.UTC().Format(time.RFC3339)}
}

func timeStr(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
