package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// backupTimestampFormat produces a filesystem-safe, lexically-sortable
// timestamp for backup filenames.
const backupTimestampFormat = "20060102-150405"

// Backup writes a consistent point-in-time copy of the store database into
// destDir using SQLite's `VACUUM INTO`, which — unlike a raw file copy —
// is safe to run against a live database (it takes a read transaction and
// produces a compacted, fully-consistent snapshot even while other
// connections are writing under WAL). Returns the path to the backup file.
func (s *Store) Backup(ctx context.Context, destDir string) (string, error) {
	if destDir == "" {
		return "", fmt.Errorf("store: destDir required")
	}
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return "", fmt.Errorf("store: mkdir backup dir: %w", err)
	}

	name := fmt.Sprintf("store-%s.db", s.clock.Now().UTC().Format(backupTimestampFormat))
	dest := filepath.Join(destDir, name)

	// VACUUM INTO requires the destination not already exist.
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("store: backup destination %s already exists", dest)
	}

	// VACUUM INTO takes a plain filesystem path as a string literal, not a
	// bound parameter (SQLite does not allow parameter binding here), so
	// the path is escaped as a SQL string literal instead. dest is built
	// entirely from destDir (caller-controlled, by design — that's the
	// whole point of Backup) joined with our own timestamped filename, so
	// this is not attacker-controlled SQL injection surface.
	//nolint:gosec // G202: VACUUM INTO has no parameter-binding form in SQLite; dest is escaped via sqlStringLiteral
	_, err := s.db.ExecContext(ctx, "VACUUM INTO "+sqlStringLiteral(dest))
	if err != nil {
		return "", fmt.Errorf("store: vacuum into: %w", err)
	}
	return dest, nil
}

// sqlStringLiteral quotes s as a SQLite string literal, doubling any
// embedded single quotes per SQL string-literal escaping rules.
func sqlStringLiteral(s string) string {
	out := make([]byte, 0, len(s)+2)
	out = append(out, '\'')
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			out = append(out, '\'', '\'')
			continue
		}
		out = append(out, s[i])
	}
	out = append(out, '\'')
	return string(out)
}
