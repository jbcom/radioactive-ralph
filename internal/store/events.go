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

// eventProjectScope is the WHERE fragment that selects a project's events,
// INCLUDING plan-scoped rows that carry only a plan_id and no project_id. The
// headline lifecycle events (task.claimed/done/failed/…) are inserted inline in
// tasks.go with plan_id/task_id only, so a bare `project_id = ?` filter would
// silently drop exactly the events a live view exists to show. plans.project_id
// is NOT NULL, so a plan-scoped event resolves to exactly one project. The
// fragment binds the project id TWICE. Rows with neither a project_id nor a
// plan_id (a few service-internal kinds like `tick`) belong to no project and
// are intentionally excluded.
const eventProjectScope = `(project_id = ? OR plan_id IN (SELECT id FROM plans WHERE project_id = ?))`

// EventsAfter returns a project's events with id strictly greater than
// afterID, in ascending id order, capped at limit. It is the tail query
// backing the Attach event stream: a client resumes from its last-seen id and
// each call returns the next contiguous batch. Ascending order is deliberate —
// events are delivered oldest-first so a live view applies them in the order
// they occurred (the opposite of ListProjectEvents, which is newest-first for
// a backlog snapshot). Pass afterID=0 to start from the beginning. Scoping
// includes plan-linked events (see eventProjectScope).
func (s *Store) EventsAfter(ctx context.Context, projectID string, afterID int64, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 256
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(project_id,''), COALESCE(plan_id,''), COALESCE(task_id,''),
		       kind, COALESCE(stream,''), COALESCE(actor,''), COALESCE(payload_json,''), occurred_at
		FROM events
		WHERE id > ? AND `+eventProjectScope+`
		ORDER BY id ASC
		LIMIT ?
	`, afterID, projectID, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: events after: %w", err)
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

// MaxEventID returns the highest event id for a project, or 0 if the project
// has no events. It gives an Attach client the initial cursor to resume from
// (the client reads this — or the backlog's max id — then attaches with it), so
// the client owns a single monotonic cursor and no event slips through the gap
// between a backlog read and the live stream. Scoping matches EventsAfter (it
// includes plan-linked rows) so the two agree on project membership.
func (s *Store) MaxEventID(ctx context.Context, projectID string) (int64, error) {
	// COALESCE folds the no-rows MAX (SQL NULL) to 0 in a single scan.
	var maxID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(id), 0) FROM events WHERE `+eventProjectScope+`
	`, projectID, projectID).Scan(&maxID)
	if err != nil {
		return 0, fmt.Errorf("store: max event id: %w", err)
	}
	return maxID, nil
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
