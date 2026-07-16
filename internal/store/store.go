// Package store owns the single user-level SQLite database that is the
// durable memory for every project the supervisor knows about: project
// identity (accumulated fingerprints, §5b of the supervisor-architecture
// design), DB-resident project config, the plan DAG, the append-only event
// log, worker/session tracking with heartbeats, and spend accounting.
//
// There is exactly ONE database per user (XDG data dir), not one per repo.
// Repos stay clean — nothing is committed by default.
//
// The schema is embedded under schema/*.sql and applied in lexical order by
// Migrate. This package is a fresh port of internal/plandag's store/task/plan
// machinery onto the new schema (project_id instead of repo_path, workers
// instead of session_variants, no persona/variant columns) with the PR #63
// safety properties (DSN _txlock=immediate + synchronous(NORMAL), checked
// RowsAffected on claims, error-checked terminal writes) carried forward.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	_ "modernc.org/sqlite" // pure-Go driver; FTS5 compiled in
)

// Store is the user-level store handle. It wraps a *sql.DB plus a
// deterministic clock + UUID provider (test-swappable).
type Store struct {
	db    *sql.DB
	clock clockwork.Clock
	uuid  func() string
}

// DSN builds the canonical modernc.org/sqlite DSN for the user-level store
// database at dbPath. Every process that opens the store (the durable
// supervisor, the TUI, and the CLI) MUST use this so they share identical
// locking and durability semantics.
//
// _txlock=immediate makes every transaction take the write lock up front, so
// a SELECT-then-UPDATE (e.g. ClaimNextReady) can never race another process
// into SQLITE_BUSY_SNAPSHOT — which busy_timeout does NOT retry.
// busy_timeout then actually serializes the concurrent writers instead of
// failing them immediately. synchronous=NORMAL is the documented-safe
// pairing with WAL and avoids an fsync on every heartbeat/tick write.
//
// The path is percent-encoded per the SQLite file: URI rules so a dbPath
// containing '?', '#', or '%' is not misparsed as URI syntax and pointed at
// the wrong database.
func DSN(dbPath string) string {
	return "file:" + escapeDSNPath(dbPath) +
		"?_txlock=immediate" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)"
}

// escapeDSNPath percent-encodes the characters that carry meaning in a
// file: URI so a filesystem path is preserved verbatim. '%' is encoded
// first so the encoding is not applied twice.
func escapeDSNPath(p string) string {
	p = strings.ReplaceAll(p, "%", "%25")
	p = strings.ReplaceAll(p, "?", "%3F")
	p = strings.ReplaceAll(p, "#", "%23")
	return p
}

// Options configures Open.
type Options struct {
	// DSN is a modernc.org/sqlite DSN. Prefer building it with DSN() so
	// every opener shares identical locking/durability pragmas.
	DSN string

	// Clock is swappable for tests. Nil defaults to clockwork.NewRealClock().
	Clock clockwork.Clock

	// UUID is swappable for tests. Nil defaults to uuid.NewV7().
	UUID func() string
}

// Open returns a migrated, ready-to-use Store.
func Open(ctx context.Context, opts Options) (*Store, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("store: DSN required")
	}
	if opts.Clock == nil {
		opts.Clock = clockwork.NewRealClock()
	}
	if opts.UUID == nil {
		opts.UUID = defaultUUID
	}

	db, err := sql.Open("sqlite", opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}

	// Verify the connection is live. sql.Open is lazy.
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	// Enable foreign keys on every connection (per-connection PRAGMA in
	// SQLite). modernc.org/sqlite honors _pragma DSN params but
	// SetMaxIdleConns > 0 can recycle connections that drop the pragma, so
	// re-affirm on an init hook. For simplicity we just exec it once;
	// Options callers pass the DSN pragmas.
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: enable FK: %w", err)
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

// defaultUUID generates a UUID v7 (time-ordered) as a lowercase 36-char
// string. v7 sorts chronologically for free, so "most recent first" queries
// can use ORDER BY id DESC without a separate index.
func defaultUUID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// Fallback to v4 — panic-free even in the (astronomically
		// unlikely) clock-skew failure mode.
		return uuid.NewString()
	}
	return id.String()
}
