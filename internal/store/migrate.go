package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/store/schema"
)

// currentSchemaVersion is bumped whenever a new NNNN_*.up.sql ships.
// The binary refuses to open a DB whose user_version exceeds this.
const currentSchemaVersion = 4

// migrationBusyAttempts and migrationBusyBackoff bound how long a losing
// concurrent opener waits for the winner's DDL to commit.
//
// Like the WAL switch, this is a backstop on top of the DSN's
// busy_timeout(5000): SQLite already blocks for the lock, so these retries only
// cover the handoff right after the winner commits. Each retry re-reads
// user_version inside its own transaction and returns as soon as it sees the
// bumped version, so a couple of quick attempts suffice. A long backoff here
// would stack onto busy_timeout and make one contended Open take tens of
// seconds.
const (
	migrationBusyAttempts = 3
	migrationBusyBackoff  = 20 * time.Millisecond
)

// ErrSchemaNewerThanBinary means the database has been migrated by a newer
// radioactive_ralph than this one. It is checked both before taking the
// migration lock and again inside it, because a newer binary can win the race
// in between.
var ErrSchemaNewerThanBinary = errors.New(
	"store: DB schema is newer than this binary supports; upgrade radioactive_ralph")

// Migrate brings db up to currentSchemaVersion by applying any pending
// *.up.sql migrations in lexical order.
//
// Migrations are idempotent per-version via SQLite's user_version PRAGMA.
// Each migration runs inside a transaction.
func Migrate(ctx context.Context, db *sql.DB) error {
	dbVersion, err := readUserVersion(db)
	if err != nil {
		return fmt.Errorf("store: read schema version: %w", err)
	}
	if dbVersion > currentSchemaVersion {
		return fmt.Errorf("%w: version %d, this binary supports %d",
			ErrSchemaNewerThanBinary, dbVersion, currentSchemaVersion)
	}

	upFiles, err := listMigrations(schema.FS, ".up.sql")
	if err != nil {
		return err
	}

	for _, m := range upFiles {
		if m.version <= dbVersion {
			continue
		}
		body, err := fs.ReadFile(schema.FS, m.name)
		if err != nil {
			return fmt.Errorf("store: read %s: %w", m.name, err)
		}
		// A concurrent first-opener may hold the write lock. _txlock=immediate
		// plus busy_timeout absorbs most of that wait, but a slow DDL under a
		// loaded runner can still exhaust it, and losing a race we are about to
		// no-op on must not fail the open.
		if err := applyMigrationWithRetry(ctx, db, m.version, string(body)); err != nil {
			return fmt.Errorf("store: apply %s: %w", m.name, err)
		}
	}
	return nil
}

// migration holds a parsed migration filename.
type migration struct {
	version int
	name    string
}

// listMigrations walks the embedded FS and returns migrations whose
// filename matches NNNN_*<suffix>, ordered by version.
func listMigrations(fsys fs.FS, suffix string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		under := strings.IndexByte(name, '_')
		if under < 1 {
			continue
		}
		v, err := strconv.Atoi(name[:under])
		if err != nil {
			continue
		}
		out = append(out, migration{version: v, name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// applyMigrationWithRetry applies one migration, retrying while the database is
// locked by a concurrent opener. Each attempt re-reads user_version inside its
// transaction, so a retry that finds the migration already applied returns
// successfully rather than re-running the DDL.
//
// The retry is cancellation-aware. Without it, a caller that cancels Open (a
// SIGTERM during shutdown, say) would keep issuing context-free transactions
// and sleeping: 20 attempts times the 5s DSN busy_timeout plus backoff can hold
// shutdown for roughly 105 seconds.
func applyMigrationWithRetry(ctx context.Context, db *sql.DB, version int, body string) error {
	var lastErr error
	for attempt := range migrationBusyAttempts {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("migration canceled: %w", err)
		}
		err := applyMigration(ctx, db, version, body)
		if err == nil {
			return nil
		}
		if !isSQLiteBusy(err) {
			return err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("migration canceled: %w", ctx.Err())
		case <-time.After(migrationBusyBackoff * time.Duration(attempt+1)):
		}
	}
	return fmt.Errorf("still busy after %d attempts: %w", migrationBusyAttempts, lastErr)
}

// applyMigration runs body inside a transaction and bumps user_version on
// success.
//
// The version is re-read INSIDE the transaction, which is what makes concurrent
// first-openers safe. Migrate's outer read happens before any lock is held, so
// several processes opening the same fresh database can all conclude "version 0,
// apply 0001". The DSN sets _txlock=immediate, so Begin here takes SQLite's
// write lock up front and exactly one writer reaches the DDL; every other
// writer re-reads the now-bumped version and skips. Without this second read
// the losers execute the same CREATE TABLE and fail with "table already
// exists", turning a routine concurrent start into a hard open failure.
func applyMigration(ctx context.Context, db *sql.DB, version int, body string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var current int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read user_version in tx: %w", err)
	}
	if current > currentSchemaVersion {
		// A NEWER binary migrated the shared database while we waited for the
		// lock. Migrate's pre-lock guard read a version this binary supports, so
		// this is the only place that can catch it. Treating it as
		// already-applied would return success and let an incompatible binary
		// read and mutate the store.
		return fmt.Errorf("%w: version %d, this binary supports %d",
			ErrSchemaNewerThanBinary, current, currentSchemaVersion)
	}
	if current >= version {
		// Another opener applied this migration while we waited for the lock.
		// Its DDL and version bump committed together, so there is nothing to do.
		return nil
	}

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return fmt.Errorf("bump user_version: %w", err)
	}

	return tx.Commit()
}

// readUserVersion returns the SQLite user_version pragma.
func readUserVersion(db *sql.DB) (int, error) {
	var v int
	err := db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}
