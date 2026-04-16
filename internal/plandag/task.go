package plandag

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TaskStatus enumerates valid task lifecycle states.
type TaskStatus string

// Task lifecycle states.
const (
	TaskStatusPending              TaskStatus = "pending"
	TaskStatusReady                TaskStatus = "ready"
	TaskStatusReadyPendingApproval TaskStatus = "ready_pending_approval"
	TaskStatusBlocked              TaskStatus = "blocked"
	TaskStatusRunning              TaskStatus = "running"
	TaskStatusDone                 TaskStatus = "done"
	TaskStatusFailed               TaskStatus = "failed"
	TaskStatusSkipped              TaskStatus = "skipped"
	TaskStatusDecomposed           TaskStatus = "decomposed"
)

// Task is a DAG node.
type Task struct {
	ID                 string
	PlanID             string
	Description        string
	Complexity         string
	Effort             string
	VariantHint        string
	ContextBoundary    bool
	AcceptanceJSON     string
	Status             TaskStatus
	AssignedVariant    string
	ClaimedBySession   string
	ClaimedByVariantID string
	RetryCount         int
	ReclaimCount       int
	ParentTaskID       string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// TaskEvent is one append-only audit-log row for a task.
type TaskEvent struct {
	ID          int64
	PlanID      string
	TaskID      string
	EventType   string
	Variant     string
	SessionID   string
	PayloadJSON string
	OccurredAt  time.Time
}

// TaskEventPayload keeps task history payloads structured so the CLI, TUI, and
// tests can reason about approvals, handoffs, retries, and provider context
// without string scraping.
type TaskEventPayload struct {
	Summary           string   `json:"summary,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	Evidence          []string `json:"evidence,omitempty"`
	HandoffTo         string   `json:"handoff_to,omitempty"`
	Retryable         bool     `json:"retryable,omitempty"`
	NeedsContext      []string `json:"needs_context,omitempty"`
	ApprovalRequired  bool     `json:"approval_required,omitempty"`
	Provider          string   `json:"provider,omitempty"`
	ProviderSessionID string   `json:"provider_session_id,omitempty"`
	OperatorAction    string   `json:"operator_action,omitempty"`
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
				_ = rows.Close()
				return false, err
			}
			next = append(next, n)
		}
		_ = rows.Close()
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
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
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

	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("plandag: task %q not found in plan %q", id, planID)
		}
		return nil, fmt.Errorf("plandag: get task: %w", err)
	}
	return &t, nil
}

// ListTasks returns tasks for one plan, optionally filtered by status.
func (s *Store) ListTasks(ctx context.Context, planID string, statuses []TaskStatus) ([]Task, error) {
	query := `
		SELECT id, plan_id, description, COALESCE(complexity,''), COALESCE(effort,''),
		       COALESCE(variant_hint,''), context_boundary, COALESCE(acceptance_json,''),
		       status, COALESCE(assigned_variant,''),
		       COALESCE(claimed_by_session,''), COALESCE(claimed_by_variant_id,''),
		       retry_count, reclaim_count, COALESCE(parent_task_id,''),
		       created_at, updated_at
		FROM tasks
		WHERE plan_id = ?
	`
	args := []any{planID}
	if len(statuses) > 0 {
		//nolint:gosec // placeholders is generated entirely from '?' tokens
		query += ` AND status IN (` + statusPlaceholders(len(statuses)) + `)`
		for _, status := range statuses {
			args = append(args, string(status))
		}
	}
	query += `
		ORDER BY
		  CASE status
		    WHEN 'ready_pending_approval' THEN 0
		    WHEN 'blocked' THEN 1
		    WHEN 'running' THEN 2
		    WHEN 'pending' THEN 3
		    WHEN 'done' THEN 4
		    WHEN 'failed' THEN 5
		    ELSE 6
		  END,
		  created_at,
		  id
	`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("plandag: list tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}

// ListTaskEvents returns the most recent task events first.
func (s *Store) ListTaskEvents(ctx context.Context, planID, taskID string, limit int) ([]TaskEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, plan_id, task_id, event_type, COALESCE(variant,''),
		       COALESCE(session_id,''), COALESCE(payload_json,''), occurred_at
		FROM task_events
		WHERE plan_id = ? AND task_id = ?
		ORDER BY occurred_at DESC, id DESC
		LIMIT ?
	`, planID, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("plandag: list task events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []TaskEvent
	for rows.Next() {
		var ev TaskEvent
		var occurredStr string
		if err := rows.Scan(
			&ev.ID, &ev.PlanID, &ev.TaskID, &ev.EventType, &ev.Variant,
			&ev.SessionID, &ev.PayloadJSON, &occurredStr,
		); err != nil {
			return nil, fmt.Errorf("plandag: scan task event: %w", err)
		}
		ev.OccurredAt = parseDBTimestamp(occurredStr)
		out = append(out, ev)
	}
	return out, rows.Err()
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
	return s.MarkFailedWithPayload(ctx, planID, taskID, sessionID, TaskEventPayload{Reason: reason}, maxRetries)
}

// MarkFailedWithPayload transitions a running task to failed or retries while
// preserving structured payload details in task history.
func (s *Store) MarkFailedWithPayload(ctx context.Context, planID, taskID, sessionID string, payload TaskEventPayload, maxRetries int) (retried bool, err error) {
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
		`, planID, taskID, sessionID, payloadJSON(payload))
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
	`, planID, taskID, sessionID, payloadJSON(payload))
	if err != nil {
		return false, err
	}
	return false, tx.Commit()
}

// RequeueTask releases a running task back into the DAG, optionally
// changing the variant hint and/or requiring operator approval before it
// becomes runnable again.
func (s *Store) RequeueTask(ctx context.Context, planID, taskID, sessionID, reason, variantHint string, requireApproval bool) error {
	return s.RequeueTaskWithPayload(ctx, planID, taskID, sessionID, TaskEventPayload{
		Reason:           reason,
		HandoffTo:        variantHint,
		ApprovalRequired: requireApproval,
	}, variantHint, requireApproval)
}

// RequeueTaskWithPayload releases a running task back into the DAG and emits a
// structured audit-log payload describing why it was requeued.
func (s *Store) RequeueTaskWithPayload(ctx context.Context, planID, taskID, sessionID string, payload TaskEventPayload, variantHint string, requireApproval bool) error {
	status := TaskStatusPending
	eventType := "requeued"
	if payload.HandoffTo != "" {
		eventType = "handoff_requested"
	}
	if requireApproval {
		status = TaskStatusReadyPendingApproval
		eventType = "approval_required"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = ?,
		    variant_hint = COALESCE(NULLIF(?, ''), variant_hint),
		    claimed_by_session = NULL,
		    claimed_by_variant_id = NULL,
		    assigned_variant = NULL
		WHERE plan_id = ? AND id = ? AND status = 'running'
	`, string(status), variantHint, planID, taskID)
	if err != nil {
		return fmt.Errorf("plandag: requeue task: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events(plan_id, task_id, event_type, session_id, payload_json)
		VALUES (?, ?, ?, ?, ?)
	`, planID, taskID, eventType, sessionID, payloadJSON(payload))
	if err != nil {
		return fmt.Errorf("plandag: log requeue: %w", err)
	}
	return tx.Commit()
}

// ApproveTask transitions a task waiting for operator approval back into
// the pending set.
func (s *Store) ApproveTask(ctx context.Context, planID, taskID string) error {
	return s.ApproveTaskWithPayload(ctx, planID, taskID, TaskEventPayload{OperatorAction: "approved"})
}

// ApproveTaskWithPayload transitions a task waiting for operator approval back
// into the pending set and records the operator action in task history.
func (s *Store) ApproveTaskWithPayload(ctx context.Context, planID, taskID string, payload TaskEventPayload) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'pending'
		WHERE plan_id = ? AND id = ? AND status = 'ready_pending_approval'
	`, planID, taskID)
	if err != nil {
		return fmt.Errorf("plandag: approve task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("plandag: task %q is not waiting for approval", taskID)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO task_events(plan_id, task_id, event_type, payload_json)
		VALUES (?, ?, 'approved', ?)
	`, planID, taskID, payloadJSON(payload))
	return err
}

// OperatorRequeueTask returns a blocked/failed/approval-gated task to the
// runnable queue and records the operator action in task history.
func (s *Store) OperatorRequeueTask(ctx context.Context, planID, taskID string, payload TaskEventPayload, variantHint string, requireApproval bool) error {
	status := TaskStatusPending
	if requireApproval {
		status = TaskStatusReadyPendingApproval
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = ?,
		    variant_hint = COALESCE(NULLIF(?, ''), variant_hint),
		    claimed_by_session = NULL,
		    claimed_by_variant_id = NULL,
		    assigned_variant = NULL
		WHERE plan_id = ? AND id = ?
		  AND status IN ('blocked', 'failed', 'ready_pending_approval', 'pending')
	`, string(status), variantHint, planID, taskID)
	if err != nil {
		return fmt.Errorf("plandag: operator requeue task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("plandag: task %q is not requeueable", taskID)
	}
	payload.OperatorAction = firstNonEmpty(payload.OperatorAction, "requeue")
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events(plan_id, task_id, event_type, payload_json)
		VALUES (?, ?, 'requeued', ?)
	`, planID, taskID, payloadJSON(payload))
	if err != nil {
		return fmt.Errorf("plandag: log operator requeue: %w", err)
	}
	return tx.Commit()
}

// OperatorRetryTask increments retry_count and returns a blocked/failed task to
// the runnable queue.
func (s *Store) OperatorRetryTask(ctx context.Context, planID, taskID string, payload TaskEventPayload) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'pending',
		    retry_count = retry_count + 1,
		    claimed_by_session = NULL,
		    claimed_by_variant_id = NULL,
		    assigned_variant = NULL
		WHERE plan_id = ? AND id = ?
		  AND status IN ('blocked', 'failed')
	`, planID, taskID)
	if err != nil {
		return fmt.Errorf("plandag: operator retry task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("plandag: task %q is not retryable from its current state", taskID)
	}
	payload.OperatorAction = firstNonEmpty(payload.OperatorAction, "retry")
	payload.Retryable = true
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events(plan_id, task_id, event_type, payload_json)
		VALUES (?, ?, 'retry_requested', ?)
	`, planID, taskID, payloadJSON(payload))
	if err != nil {
		return fmt.Errorf("plandag: log operator retry: %w", err)
	}
	return tx.Commit()
}

// OperatorHandoffTask returns a task to the runnable queue with a new variant
// hint supplied by the operator.
func (s *Store) OperatorHandoffTask(ctx context.Context, planID, taskID string, payload TaskEventPayload, variantHint string, requireApproval bool) error {
	payload.HandoffTo = variantHint
	payload.OperatorAction = firstNonEmpty(payload.OperatorAction, "handoff")
	return s.OperatorRequeueTask(ctx, planID, taskID, payload, variantHint, requireApproval)
}

// OperatorFailTask force-fails a task and records an operator action.
func (s *Store) OperatorFailTask(ctx context.Context, planID, taskID string, payload TaskEventPayload) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'failed',
		    claimed_by_session = NULL,
		    claimed_by_variant_id = NULL,
		    assigned_variant = NULL
		WHERE plan_id = ? AND id = ?
		  AND status NOT IN ('done', 'skipped', 'decomposed')
	`, planID, taskID)
	if err != nil {
		return fmt.Errorf("plandag: operator fail task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("plandag: task %q cannot be force-failed", taskID)
	}
	payload.OperatorAction = firstNonEmpty(payload.OperatorAction, "fail")
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO task_events(plan_id, task_id, event_type, payload_json)
		VALUES (?, ?, 'failed_terminal', ?)
	`, planID, taskID, payloadJSON(payload))
	return err
}

// MarkBlocked releases a running task into the blocked set so an operator can
// later requeue or otherwise intervene.
func (s *Store) MarkBlocked(ctx context.Context, planID, taskID, sessionID string, payload TaskEventPayload) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'blocked',
		    claimed_by_session = NULL,
		    claimed_by_variant_id = NULL,
		    assigned_variant = NULL
		WHERE plan_id = ? AND id = ? AND status = 'running'
	`, planID, taskID)
	if err != nil {
		return fmt.Errorf("plandag: block task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("plandag: task %q not in running state", taskID)
	}

	eventType := "blocked"
	if len(payload.NeedsContext) > 0 {
		eventType = "context_requested"
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_events(plan_id, task_id, event_type, session_id, payload_json)
		VALUES (?, ?, ?, ?, ?)
	`, planID, taskID, eventType, sessionID, payloadJSON(payload))
	if err != nil {
		return fmt.Errorf("plandag: log block: %w", err)
	}
	return tx.Commit()
}

func payloadJSON(payload TaskEventPayload) string {
	if isZeroPayload(payload) {
		return "{}"
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func isZeroPayload(payload TaskEventPayload) bool {
	return payload.Summary == "" &&
		payload.Reason == "" &&
		len(payload.Evidence) == 0 &&
		payload.HandoffTo == "" &&
		!payload.Retryable &&
		len(payload.NeedsContext) == 0 &&
		!payload.ApprovalRequired &&
		payload.Provider == "" &&
		payload.ProviderSessionID == "" &&
		payload.OperatorAction == ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (Task, error) {
	var t Task
	var cb int
	var createdStr, updatedStr string
	err := scanner.Scan(
		&t.ID, &t.PlanID, &t.Description, &t.Complexity, &t.Effort,
		&t.VariantHint, &cb, &t.AcceptanceJSON,
		&t.Status, &t.AssignedVariant,
		&t.ClaimedBySession, &t.ClaimedByVariantID,
		&t.RetryCount, &t.ReclaimCount, &t.ParentTaskID,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return Task{}, err
	}
	t.ContextBoundary = cb == 1
	t.CreatedAt = parseDBTimestamp(createdStr)
	t.UpdatedAt = parseDBTimestamp(updatedStr)
	return t, nil
}

func scanTasks(rows *sql.Rows) ([]Task, error) {
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func statusPlaceholders(n int) string {
	return strings.TrimPrefix(strings.Repeat(",?", n), ",")
}
