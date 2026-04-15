package plandag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TaskStatus enumerates valid task lifecycle states.
type TaskStatus string

const (
	TaskStatusPending              TaskStatus = "pending"
	TaskStatusReady                TaskStatus = "ready"
	TaskStatusReadyPendingApproval TaskStatus = "ready_pending_approval"
	TaskStatusRunning              TaskStatus = "running"
	TaskStatusDone                 TaskStatus = "done"
	TaskStatusFailed               TaskStatus = "failed"
	TaskStatusSkipped              TaskStatus = "skipped"
	TaskStatusDecomposed           TaskStatus = "decomposed"
)

// Task is a DAG node.
type Task struct {
	ID                  string
	PlanID              string
	Description         string
	Complexity          string
	Effort              string
	VariantHint         string
	ContextBoundary     bool
	AcceptanceJSON      string
	Status              TaskStatus
	AssignedVariant     string
	ClaimedBySession    string
	ClaimedByVariantID  string
	RetryCount          int
	ReclaimCount        int
	ParentTaskID        string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// CreateTaskOpts configures task creation.
type CreateTaskOpts struct {
	PlanID          string
	ID              string // stable slug (operator-chosen or fixit-emitted)
	Description     string
	Complexity      string
	Effort          string
	VariantHint     string
	ContextBoundary bool
	AcceptanceJSON  string
	ParentTaskID    string
}

// CreateTask inserts a pending task. Callers wire dependencies via AddDep.
func (s *Store) CreateTask(ctx context.Context, o CreateTaskOpts) error {
	if o.PlanID == "" || o.ID == "" || o.Description == "" {
		return fmt.Errorf("plandag: PlanID, ID, and Description required")
	}
	cb := 0
	if o.ContextBoundary {
		cb = 1
	}

	var parent sql.NullString
	if o.ParentTaskID != "" {
		parent = sql.NullString{String: o.ParentTaskID, Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks(
			id, plan_id, description, complexity, effort,
			variant_hint, context_boundary, acceptance_json,
			status, parent_task_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?)
	`, o.ID, o.PlanID, o.Description, o.Complexity, o.Effort,
		o.VariantHint, cb, o.AcceptanceJSON, parent)
	if err != nil {
		return fmt.Errorf("plandag: insert task: %w", err)
	}
	return nil
}

// AddDep wires task → depends_on for the same plan. Rejects cycles.
func (s *Store) AddDep(ctx context.Context, planID, taskID, dependsOn string) error {
	if taskID == dependsOn {
		return fmt.Errorf("plandag: task cannot depend on itself")
	}
	// Reject cycles by checking: does `depends_on` transitively depend
	// on `task_id`? If yes, adding this edge creates a cycle.
	if wouldCycle, err := s.wouldCreateCycle(ctx, planID, taskID, dependsOn); err != nil {
		return fmt.Errorf("plandag: cycle check: %w", err)
	} else if wouldCycle {
		return fmt.Errorf("plandag: adding dep %s → %s would create a cycle", taskID, dependsOn)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO task_deps(plan_id, task_id, depends_on) VALUES (?, ?, ?)`,
		planID, taskID, dependsOn)
	if err != nil {
		return fmt.Errorf("plandag: insert dep: %w", err)
	}
	return nil
}

// wouldCreateCycle returns true if dep (task→depends_on) would make
// the graph cyclic. DFS from depends_on; if we can reach task, cycle.
func (s *Store) wouldCreateCycle(ctx context.Context, planID, task, dep string) (bool, error) {
	visited := map[string]bool{}
	var visit func(node string) (bool, error)
	visit = func(node string) (bool, error) {
		if node == task {
			return true, nil
		}
		if visited[node] {
			return false, nil
		}
		visited[node] = true
		rows, err := s.db.QueryContext(ctx,
			`SELECT depends_on FROM task_deps WHERE plan_id = ? AND task_id = ?`,
			planID, node)
		if err != nil {
			return false, err
		}
		var next []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return false, err
			}
			next = append(next, n)
		}
		rows.Close()
		for _, n := range next {
			cyc, err := visit(n)
			if err != nil {
				return false, err
			}
			if cyc {
				return true, nil
			}
		}
		return false, nil
	}
	return visit(dep)
}

// Ready returns tasks that are ready to run — every dependency is
// `done` (or `skipped`). Result is ordered by created_at for
// stable test output.
func (s *Store) Ready(ctx context.Context, planID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, plan_id, description, COALESCE(complexity,''), COALESCE(effort,''),
		       COALESCE(variant_hint,''), context_boundary, COALESCE(acceptance_json,''),
		       status, COALESCE(assigned_variant,''),
		       COALESCE(claimed_by_session,''), COALESCE(claimed_by_variant_id,''),
		       retry_count, reclaim_count, COALESCE(parent_task_id,''),
		       created_at, updated_at
		FROM tasks
		WHERE plan_id = ?
		  AND status = 'pending'
		  AND NOT EXISTS (
		    SELECT 1 FROM task_deps d
		     JOIN tasks tdep ON tdep.plan_id = d.plan_id AND tdep.id = d.depends_on
		    WHERE d.plan_id = tasks.plan_id
		      AND d.task_id = tasks.id
		      AND tdep.status NOT IN ('done', 'skipped')
		  )
		ORDER BY created_at
	`, planID)
	if err != nil {
		return nil, fmt.Errorf("plandag: query ready: %w", err)
	}
	defer rows.Close()

	var out []Task
	for rows.Next() {
		var t Task
		var cb int
		var createdStr, updatedStr string
		if err := rows.Scan(
			&t.ID, &t.PlanID, &t.Description, &t.Complexity, &t.Effort,
			&t.VariantHint, &cb, &t.AcceptanceJSON,
			&t.Status, &t.AssignedVariant,
			&t.ClaimedBySession, &t.ClaimedByVariantID,
			&t.RetryCount, &t.ReclaimCount, &t.ParentTaskID,
			&createdStr, &updatedStr,
		); err != nil {
			return nil, err
		}
		t.ContextBoundary = cb == 1
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ClaimNextReady is the atomic "claim the next ready task for this
// variant" operation. Returns the claimed task id, or ErrNoReadyTask
// if none. Uses BEGIN IMMEDIATE + UPDATE ... RETURNING so two
// parallel ralphs never claim the same task.
func (s *Store) ClaimNextReady(
	ctx context.Context,
	planID, variant, sessionID, sessionVariantID string,
) (*Task, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("plandag: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Find the first ready task that matches variant_hint (if set)
	// or has no hint. Hint-matching is soft — if none match, we take
	// an unmatched ready task.
	selectQ := `
		SELECT id FROM tasks
		WHERE plan_id = ?
		  AND status = 'pending'
		  AND NOT EXISTS (
		    SELECT 1 FROM task_deps d
		     JOIN tasks tdep ON tdep.plan_id = d.plan_id AND tdep.id = d.depends_on
		    WHERE d.plan_id = tasks.plan_id
		      AND d.task_id = tasks.id
		      AND tdep.status NOT IN ('done', 'skipped')
		  )
		ORDER BY
		  CASE WHEN variant_hint = ? THEN 0
		       WHEN variant_hint = ''   THEN 1
		       ELSE 2 END,
		  created_at
		LIMIT 1
	`
	var taskID string
	if err := tx.QueryRowContext(ctx, selectQ, planID, variant).Scan(&taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoReadyTask
		}
		return nil, fmt.Errorf("plandag: select ready: %w", err)
	}

	// Atomic claim: set status=running, assigned_variant,
	// claimed_by_session, claimed_by_variant_id.
	_, err = tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'running',
		    assigned_variant = ?,
		    claimed_by_session = ?,
		    claimed_by_variant_id = ?
		WHERE plan_id = ? AND id = ? AND status = 'pending'
	`, variant, sessionID, sessionVariantID, planID, taskID)
	if err != nil {
		return nil, fmt.Errorf("plandag: claim update: %w", err)
	}

	// Emit event for audit log.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events(plan_id, task_id, event_type, variant, session_id)
		VALUES (?, ?, 'claimed', ?, ?)
	`, planID, taskID, variant, sessionID)
	if err != nil {
		return nil, fmt.Errorf("plandag: log claim: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("plandag: commit claim: %w", err)
	}

	// Fetch the now-claimed row for the return value.
	return s.GetTask(ctx, planID, taskID)
}

// ErrNoReadyTask indicates ClaimNextReady found nothing claimable.
var ErrNoReadyTask = errors.New("plandag: no ready task")

// GetTask loads one task by (plan_id, id).
func (s *Store) GetTask(ctx context.Context, planID, id string) (*Task, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, plan_id, description, COALESCE(complexity,''), COALESCE(effort,''),
		       COALESCE(variant_hint,''), context_boundary, COALESCE(acceptance_json,''),
		       status, COALESCE(assigned_variant,''),
		       COALESCE(claimed_by_session,''), COALESCE(claimed_by_variant_id,''),
		       retry_count, reclaim_count, COALESCE(parent_task_id,''),
		       created_at, updated_at
		FROM tasks WHERE plan_id = ? AND id = ?
	`, planID, id)

	var t Task
	var cb int
	var createdStr, updatedStr string
	if err := row.Scan(
		&t.ID, &t.PlanID, &t.Description, &t.Complexity, &t.Effort,
		&t.VariantHint, &cb, &t.AcceptanceJSON,
		&t.Status, &t.AssignedVariant,
		&t.ClaimedBySession, &t.ClaimedByVariantID,
		&t.RetryCount, &t.ReclaimCount, &t.ParentTaskID,
		&createdStr, &updatedStr,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("plandag: task %q not found in plan %q", id, planID)
		}
		return nil, fmt.Errorf("plandag: get task: %w", err)
	}
	t.ContextBoundary = cb == 1
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return &t, nil
}

// MarkDone transitions a running task to done, logs the event, and
// returns the set of newly-ready downstream tasks.
func (s *Store) MarkDone(ctx context.Context, planID, taskID, sessionID string, evidenceJSON string) ([]Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = 'done',
		                 claimed_by_session = NULL,
		                 claimed_by_variant_id = NULL
		WHERE plan_id = ? AND id = ? AND status = 'running'
	`, planID, taskID)
	if err != nil {
		return nil, fmt.Errorf("plandag: update: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, fmt.Errorf("plandag: task %q not in running state", taskID)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events(plan_id, task_id, event_type, session_id, payload_json)
		VALUES (?, ?, 'completed', ?, ?)
	`, planID, taskID, sessionID, strings.TrimSpace(evidenceJSON))
	if err != nil {
		return nil, fmt.Errorf("plandag: log done: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("plandag: commit: %w", err)
	}

	// Return the new ready set so callers can hand the next task to a
	// variant without a separate round-trip.
	return s.Ready(ctx, planID)
}

// MarkFailed transitions a running task to failed or retries.
func (s *Store) MarkFailed(ctx context.Context, planID, taskID, sessionID, reason string, maxRetries int) (retried bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var retries int
	if err := tx.QueryRowContext(ctx,
		`SELECT retry_count FROM tasks WHERE plan_id = ? AND id = ?`,
		planID, taskID).Scan(&retries); err != nil {
		return false, fmt.Errorf("plandag: read retry_count: %w", err)
	}

	if retries+1 <= maxRetries {
		_, err = tx.ExecContext(ctx, `
			UPDATE tasks
			SET status = 'pending',
			    retry_count = retry_count + 1,
			    claimed_by_session = NULL,
			    claimed_by_variant_id = NULL
			WHERE plan_id = ? AND id = ?
		`, planID, taskID)
		if err != nil {
			return false, fmt.Errorf("plandag: requeue: %w", err)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO task_events(plan_id, task_id, event_type, session_id, payload_json)
			VALUES (?, ?, 'failed', ?, ?)
		`, planID, taskID, sessionID, jsonReason(reason))
		if err != nil {
			return false, err
		}
		return true, tx.Commit()
	}

	// Out of retries.
	_, err = tx.ExecContext(ctx, `
		UPDATE tasks SET status = 'failed',
		                 claimed_by_session = NULL,
		                 claimed_by_variant_id = NULL
		WHERE plan_id = ? AND id = ?
	`, planID, taskID)
	if err != nil {
		return false, fmt.Errorf("plandag: mark failed: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events(plan_id, task_id, event_type, session_id, payload_json)
		VALUES (?, ?, 'failed', ?, ?)
	`, planID, taskID, sessionID, jsonReason(reason))
	if err != nil {
		return false, err
	}
	return false, tx.Commit()
}

// jsonReason produces a minimal JSON payload capturing a reason
// string. Keeps payload structure consistent so `ralph plan history`
// can read it uniformly.
func jsonReason(reason string) string {
	// Escape double quotes by falling back to SQLite's json() via
	// ad-hoc literal; for correctness use encoding/json.
	if reason == "" {
		return "{}"
	}
	esc := strings.ReplaceAll(reason, `"`, `\"`)
	return `{"reason":"` + esc + `"}`
}
