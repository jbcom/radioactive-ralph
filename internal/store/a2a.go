package store

import (
	"context"
	"fmt"
)

// A2AMessage is one durable row in the a2a_messages evidence/message log.
// content_json holds the serialized a2a.Message (see internal/a2a); this
// package treats it as an opaque string so internal/store has no import
// dependency on internal/a2a.
type A2AMessage struct {
	ID          int64
	WorkerID    string
	PlanID      string
	TaskID      string
	Role        string
	ContentJSON string
	OccurredAt  string
}

// AppendMessageOpts configures AppendMessage.
type AppendMessageOpts struct {
	WorkerID    string // optional
	PlanID      string
	TaskID      string
	Role        string // e.g. "ROLE_AGENT" | "ROLE_USER"
	ContentJSON string // the serialized a2a.Message
}

// AppendMessage records one worker<->orchestrator A2A message (most
// importantly, evidence a worker submits when it believes a task is done).
// This ONLY logs the message — it never changes task status. Only
// internal/orch.VerifyAndComplete may transition a task to done.
func (s *Store) AppendMessage(ctx context.Context, o AppendMessageOpts) error {
	if o.PlanID == "" || o.TaskID == "" || o.Role == "" || o.ContentJSON == "" {
		return fmt.Errorf("store: PlanID, TaskID, Role, and ContentJSON required")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO a2a_messages(worker_id, plan_id, task_id, role, content_json)
		VALUES (?, ?, ?, ?, ?)
	`, nullIfEmpty(o.WorkerID), o.PlanID, o.TaskID, o.Role, o.ContentJSON)
	if err != nil {
		return fmt.Errorf("store: append a2a message: %w", err)
	}
	return nil
}

// ListMessages returns every message logged for one task, oldest first.
func (s *Store) ListMessages(ctx context.Context, planID, taskID string) ([]A2AMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(worker_id,''), plan_id, task_id, role, content_json, occurred_at
		FROM a2a_messages
		WHERE plan_id = ? AND task_id = ?
		ORDER BY occurred_at, id
	`, planID, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: list a2a messages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []A2AMessage
	for rows.Next() {
		var m A2AMessage
		if err := rows.Scan(&m.ID, &m.WorkerID, &m.PlanID, &m.TaskID, &m.Role, &m.ContentJSON, &m.OccurredAt); err != nil {
			return nil, fmt.Errorf("store: scan a2a message: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
