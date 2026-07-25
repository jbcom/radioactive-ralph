package store

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// GetProjectConfig returns all DB-resident config key/value pairs for a
// project. Values are stored as JSON-encoded scalars/arrays/objects (the
// caller decodes); this layer treats them as opaque strings.
func (s *Store) GetProjectConfig(ctx context.Context, projectID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, value FROM project_config WHERE project_id = ?
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: get project config: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("store: scan project config: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetProjectConfig upserts one DB-resident config key/value pair for a
// project.
func (s *Store) SetProjectConfig(ctx context.Context, projectID, key, value string) error {
	return s.ApplyProjectConfig(ctx, projectID, map[string]string{key: value}, nil)
}

// ApplyProjectConfig atomically deletes and upserts a set of DB-resident
// project config keys. Deletes run before upserts, so a key present in both
// collections ends with the upserted value. This is the mutation primitive
// for replacing logical config selections whose old aliases must not survive
// beside their canonical key.
func (s *Store) ApplyProjectConfig(ctx context.Context, projectID string, upserts map[string]string, deleteKeys []string) error {
	if projectID == "" {
		return fmt.Errorf("store: projectID required")
	}
	for key := range upserts {
		if key == "" {
			return fmt.Errorf("store: config upsert key required")
		}
	}
	for _, key := range deleteKeys {
		if key == "" {
			return fmt.Errorf("store: config delete key required")
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin project config: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	sortedDeletes := append([]string(nil), deleteKeys...)
	sort.Strings(sortedDeletes)
	for _, key := range sortedDeletes {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM project_config WHERE project_id = ? AND key = ?`,
			projectID, key,
		); err != nil {
			return fmt.Errorf("store: delete project config %q: %w", key, err)
		}
	}

	now := s.clock.Now().UTC().Format(time.RFC3339)
	sortedUpserts := make([]string, 0, len(upserts))
	for key := range upserts {
		sortedUpserts = append(sortedUpserts, key)
	}
	sort.Strings(sortedUpserts)
	for _, key := range sortedUpserts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO project_config(project_id, key, value, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(project_id, key) DO UPDATE SET
				value = excluded.value,
				updated_at = excluded.updated_at
		`, projectID, key, upserts[key], now); err != nil {
			return fmt.Errorf("store: upsert project config %q: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit project config: %w", err)
	}
	return nil
}
