package plandag

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	_ "modernc.org/sqlite" // pure-Go driver; FTS5 compiled in
)

// Store is the plandag handle. It wraps a *sql.DB plus deterministic
// clock + UUID provider (test-swappable).
type Store struct {
	db    *sql.DB
	clock clockwork.Clock
	uuid  func() string
}

// Options configures Open.
type Options struct {
	// DSN is a modernc.org/sqlite DSN, e.g.
	// "file:/path/to/plans.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	DSN string

	// Clock is swappable for tests. Nil defaults to clockwork.NewRealClock().
	Clock clockwork.Clock

	// UUID is swappable for tests. Nil defaults to uuid.NewV7().
	UUID func() string
}

// Open returns a migrated, ready-to-use Store.
func Open(ctx context.Context, opts Options) (*Store, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("plandag: DSN required")
	}
	if opts.Clock == nil {
		opts.Clock = clockwork.NewRealClock()
	}
	if opts.UUID == nil {
		opts.UUID = defaultUUID
	}

	db, err := sql.Open("sqlite", opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("plandag: open: %w", err)
	}

	// Verify the connection is live. sql.Open is lazy.
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("plandag: ping: %w", err)
	}

	// Enable foreign keys on every connection (per-connection PRAGMA
	// in SQLite). modernc.org/sqlite honors _pragma DSN params but
	// SetMaxIdleConns > 0 can recycle connections that drop the
	// pragma, so re-affirm on an init hook. For simplicity we just
	// exec it once; Options callers pass the DSN pragmas.
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("plandag: enable FK: %w", err)
	}

	if err := Migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db, clock: opts.Clock, uuid: opts.UUID}, nil
}

// Close releases DB resources.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying *sql.DB for callers that need raw access.
// Business-logic callers should use Store's typed methods instead.
func (s *Store) DB() *sql.DB {
	return s.db
}

// defaultUUID generates a UUID v7 (time-ordered) as a lowercase
// 36-char string. v7 sorts chronologically for free, so "most recent
// first" queries can use ORDER BY id DESC without a separate index.
func defaultUUID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// Fallback to v4 — panic-free even in the (astronomically
		// unlikely) clock-skew failure mode.
		return uuid.NewString()
	}
	return id.String()
}
