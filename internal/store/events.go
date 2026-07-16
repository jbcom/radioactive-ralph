package store

import (
	"context"
	"fmt"
	"time"
)

// Event is one append-only audit-log row. Project-scoped; may additionally
// be plan/task-scoped. Both the old per-repo event log and task_events
// collapse into this single table (see schema §events).
type Event struct {
	ID          int64
	ProjectID   string
	PlanID      string
	TaskID      string
	Kind        string
	Stream      string
	Actor       string
	PayloadJSON string
	OccurredAt  time.Time
}

// EmitOpts configures Emit.
type EmitOpts struct {
	ProjectID   string
	PlanID      string
	TaskID      string
	Kind        string
	Stream      string
	Actor       string
	PayloadJSON string
}

// Emit appends one event row. Used for events not already covered by a more
// specific transactional helper (e.g. task claim/done/failed emit their own
// events inline so the status transition and audit row commit atomically).
func (s *Store) Emit(ctx context.Context, o EmitOpts) error {
	if o.Kind == "" {
		return fmt.Errorf("store: Kind required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO events(project_id, plan_id, task_id, kind, stream, actor, payload_json)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, nullIfEmpty(o.ProjectID), nullIfEmpty(o.PlanID), nullIfEmpty(o.TaskID),
		o.Kind, nullIfEmpty(o.Stream), nullIfEmpty(o.Actor), jsonOrEmptyObject(o.PayloadJSON))
	if err != nil {
		return fmt.Errorf("store: emit event: %w", err)
	}
	return nil
}

// ListProjectEvents returns the most recent events for one project first.
func (s *Store) ListProjectEvents(ctx context.Context, projectID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(project_id,''), COALESCE(plan_id,''), COALESCE(task_id,''),
		       kind, COALESCE(stream,''), COALESCE(actor,''), COALESCE(payload_json,''), occurred_at
		FROM events
		WHERE project_id = ?
		ORDER BY occurred_at DESC, id DESC
		LIMIT ?
	`, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list project events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan event: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

type eventScanner interface {
	Scan(dest ...any) error
}

func scanEvent(scanner eventScanner) (Event, error) {
	var ev Event
	var occurredStr string
	err := scanner.Scan(
		&ev.ID, &ev.ProjectID, &ev.PlanID, &ev.TaskID, &ev.Kind, &ev.Stream,
		&ev.Actor, &ev.PayloadJSON, &occurredStr,
	)
	if err != nil {
		return Event{}, err
	}
	ev.OccurredAt = parseDBTimestamp(occurredStr)
	return ev, nil
}
