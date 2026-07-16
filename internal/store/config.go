package store

import (
	"context"
	"fmt"
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
	if projectID == "" || key == "" {
		return fmt.Errorf("store: projectID and key required")
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO project_config(project_id, key, value, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(project_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, projectID, key, value, now)
	if err != nil {
		return fmt.Errorf("store: set project config: %w", err)
	}
	return nil
}
