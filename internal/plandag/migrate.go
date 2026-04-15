// Package plandag owns the SQLite schema, DAG operations, and
// sync protocol for user-facing plans.
//
// The schema is embedded under schema/*.sql and applied in lexical
// order by Migrate. The schema_version is tracked in a dedicated
// table so future versions can detect skew and refuse to open a
// DB newer than the running binary supports.
package plandag

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/plandag/schema"
)

// currentSchemaVersion is bumped whenever a new NNNN_*.up.sql ships.
// The binary refuses to open a DB whose user_version exceeds this.
const currentSchemaVersion = 1

// Migrate brings db up to currentSchemaVersion by applying any
// pending *.up.sql migrations in lexical order.
//
// Migrations are idempotent per-version via SQLite's
// user_version PRAGMA. Each migration runs inside a transaction.
func Migrate(db *sql.DB) error {
	dbVersion, err := readUserVersion(db)
	if err != nil {
		return fmt.Errorf("plandag: read schema version: %w", err)
	}
	if dbVersion > currentSchemaVersion {
		return fmt.Errorf(
			"plandag: DB schema version %d is newer than this binary supports (%d); upgrade radioactive_ralph",
			dbVersion, currentSchemaVersion)
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
			return fmt.Errorf("plandag: read %s: %w", m.name, err)
		}
		if err := applyMigration(db, m.version, string(body)); err != nil {
			return fmt.Errorf("plandag: apply %s: %w", m.name, err)
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

// applyMigration runs body inside a transaction and bumps
// user_version on success.
func applyMigration(db *sql.DB, version int, body string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(body); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
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
