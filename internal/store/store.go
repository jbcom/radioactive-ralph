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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	"modernc.org/sqlite"
	_ "modernc.org/sqlite" // pure-Go driver; FTS5 compiled in
)

// maxDBConns caps the database/sql pool. Small enough to bound writer-lock
// contention (see Open), large enough that a live VACUUM INTO backup or a
// long-held reader can't starve concurrent store calls — and >1 to avoid
// database/sql's single-connection deadlock.
const maxDBConns = 4

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
// journal_mode is deliberately NOT a DSN _pragma. It is a database-wide,
// persistent setting, but a DSN _pragma runs on every newly opened pooled
// connection, and setting journal_mode takes a lock on the database. With a
// pool, that turns each new connection into a lock acquisition that can lose to
// a concurrent writer and fail the open with SQLITE_BUSY. Open sets it once,
// after Ping, via ensureJournalModeWAL.
func DSN(dbPath string) string {
	return "file:" + escapeDSNPath(dbPath) +
		"?_txlock=immediate" +
		"&_pragma=foreign_keys(1)" +
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

	// Bound the connection pool to a small fixed size. _txlock=immediate makes
	// every BeginTx take SQLite's one writer lock up front, so an UNCAPPED pool
	// lets unbounded goroutines (the 1s dispatch tick, every per-connection IPC
	// handler, each dispatched worker's store writes, the reaper) each open a
	// fresh connection and pile onto that lock, with only busy_timeout(5s) as the
	// backstop — a SQLITE_BUSY under load surfaces as a hard "database is locked"
	// deep in a store call. A small cap keeps that contention bounded.
	//
	// NOT 1: a single connection deadlocks database/sql if any code issues a
	// query while another is still using the sole connection (e.g. a long Rows
	// held open), and it lets a long-running VACUUM INTO (Store.Backup) monopolize
	// the DB, freezing every heartbeat/dispatch/reaper/IPC store call for the whole
	// backup. maxDBConns leaves headroom for a live backup + concurrent readers
	// (WAL allows concurrent reads; the single writer is still serialized by
	// _txlock=immediate + busy_timeout). This is a low-throughput control-plane DB,
	// so the exact number is not performance-critical — it exists to cap contention
	// without the single-connection hazards.
	db.SetMaxOpenConns(maxDBConns)

	// Verify the connection is live. sql.Open is lazy.
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	// The DSN sets foreign_keys(1) as a per-connection _pragma, so every pooled
	// connection enforces FKs. This one-time exec is a cheap belt-and-suspenders
	// that documents the invariant at the Go layer (it lands on whichever pooled
	// connection serves it; the DSN pragma covers the rest).
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: enable FK: %w", err)
	}

	if err := ensureJournalModeWAL(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db, clock: opts.Clock, uuid: opts.UUID}, nil
}

// journalModeWALAttempts and journalModeWALBackoff bound the retry window for
// the one-time WAL switch.
//
// This is a thin BACKSTOP, not a second timeout. The DSN already sets
// busy_timeout(5000), so SQLite itself blocks up to 5s waiting for the lock
// before it ever returns SQLITE_BUSY; these retries only cover the narrow case
// where a concurrent first-opener commits the mode switch right as this one
// gives up. Stacking a long backoff on top of busy_timeout would make a single
// contended Open take tens of seconds — the winner's switch is persistent, so a
// loser needs one or two quick re-reads, not a long wait.
const (
	journalModeWALAttempts = 3
	journalModeWALBackoff  = 20 * time.Millisecond

	// SQLite primary result codes for the two retryable lock conditions.
	sqlite3BusyCode   = 5
	sqlite3LockedCode = 6
)

// ensureJournalModeWAL sets the database-wide WAL journal mode exactly once per
// Open, after the pool is live. journal_mode cannot be a DSN _pragma: it would
// re-run on every newly opened pooled connection, and because setting it takes
// a database lock, a pool under load would turn routine connection creation
// into a lock acquisition that can fail with SQLITE_BUSY.
//
// The pragma returns the resulting mode, so success is verified by reading it
// back rather than by trusting a nil error. A database already in WAL is the
// common case on reopen and returns "wal" immediately.
func ensureJournalModeWAL(ctx context.Context, db *sql.DB) error {
	var lastErr error
	for attempt := range journalModeWALAttempts {
		var mode string
		// PRAGMA journal_mode returns the mode it settled on.
		err := db.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&mode)
		if err == nil {
			if strings.EqualFold(mode, "wal") {
				return nil
			}
			// A non-WAL result with no error means something else owns the
			// mode (an in-memory or temp database cannot be WAL). Report the
			// mode instead of silently continuing on the wrong durability.
			return fmt.Errorf("store: journal_mode = %q, want wal", mode)
		}
		lastErr = err
		if !isSQLiteBusy(err) {
			return fmt.Errorf("store: set journal_mode=WAL: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("store: set journal_mode=WAL: %w", ctx.Err())
		case <-time.After(journalModeWALBackoff * time.Duration(attempt+1)):
		}
	}
	return fmt.Errorf(
		"store: set journal_mode=WAL: still busy after %d attempts: %w",
		journalModeWALAttempts, lastErr)
}

// isSQLiteBusy reports whether err is a SQLITE_BUSY/SQLITE_LOCKED condition,
// which is retryable, as opposed to a structural failure that is not.
//
// modernc.org/sqlite reports these through *sqlite.Error with the SQLite
// primary result code, so match on the code. Bare substring matching on the
// message is deliberately avoided: the driver's text varies by call site, and
// matching loose fragments risks retrying a non-busy error forever.
func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	var serr *sqlite.Error
	if errors.As(err, &serr) {
		// Primary result codes; extended codes share the low byte.
		switch serr.Code() & 0xff {
		case sqlite3BusyCode, sqlite3LockedCode:
			return true
		}
	}
	// Fallback for errors that lost their type crossing a boundary.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked")
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
