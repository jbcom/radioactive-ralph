package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

// Task is a DAG node. No variant/persona columns — the orchestrator assigns
// a worker from plan structure, not from a baked persona (§10 of the
// supervisor-architecture design).
type Task struct {
	ID                string
	PlanID            string
	Description       string
	Status            TaskStatus
	ParallelGroup     sql.NullInt64
	SequenceOrdinal   sql.NullInt64
	AcceptanceJSON    string
	ClaimedBySession  string
	ClaimedByWorkerID string
	RetryCount        int
	ReclaimCount      int
	ParentTaskID      string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// EventPayload keeps event payloads structured so the CLI, TUI, and tests
// can reason about approvals, handoffs, retries, and provider context
// without string scraping.
type EventPayload struct {
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
	ParallelGroup   *int64
	SequenceOrdinal *int64
	AcceptanceJSON  string
	ParentTaskID    string
}

// ErrDuplicateTask is returned by CreateTask when a task with the same
// (plan_id, id) already exists. It is the ONE benign CreateTask failure a
// concurrent dispatcher expects (two dispatchers racing to materialize the
// same step); callers match it via errors.Is and fall through to the claim
// attempt. Every OTHER CreateTask error (disk full, DB busy, I/O, a
// validation failure) is a real fault that must NOT be silently swallowed.
var ErrDuplicateTask = errors.New("store: task with this id already exists in this plan")

// CreateTask inserts a pending task. Callers wire dependencies via AddDep.
func (s *Store) CreateTask(ctx context.Context, o CreateTaskOpts) error {
	if o.PlanID == "" || o.ID == "" || o.Description == "" {
		return fmt.Errorf("store: PlanID, ID, and Description required")
	}

	var parallelGroup, sequenceOrdinal sql.NullInt64
	if o.ParallelGroup != nil {
		parallelGroup = sql.NullInt64{Int64: *o.ParallelGroup, Valid: true}
	}
	if o.SequenceOrdinal != nil {
		sequenceOrdinal = sql.NullInt64{Int64: *o.SequenceOrdinal, Valid: true}
	}

	var parent sql.NullString
	if o.ParentTaskID != "" {
		parent = sql.NullString{String: o.ParentTaskID, Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO tasks(
			id, plan_id, description, parallel_group, sequence_ordinal,
			acceptance_json, status, parent_task_id
		) VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)
	`, o.ID, o.PlanID, o.Description, parallelGroup, sequenceOrdinal,
		nullIfEmpty(o.AcceptanceJSON), parent)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: %s (plan=%s)", ErrDuplicateTask, o.ID, o.PlanID)
		}
		return fmt.Errorf("store: insert task: %w", err)
	}
	return nil
}

// AddDep wires task → depends_on for the same plan. Rejects cycles.
func (s *Store) AddDep(ctx context.Context, planID, taskID, dependsOn string) error {
	if taskID == dependsOn {
		return fmt.Errorf("store: task cannot depend on itself")
	}
	// Reject cycles by checking: does `depends_on` transitively depend on
	// `task_id`? If yes, adding this edge creates a cycle.
	if wouldCycle, err := s.wouldCreateCycle(ctx, planID, taskID, dependsOn); err != nil {
		return fmt.Errorf("store: cycle check: %w", err)
	} else if wouldCycle {
		return fmt.Errorf("store: adding dep %s → %s would create a cycle", taskID, dependsOn)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO task_deps(plan_id, task_id, depends_on) VALUES (?, ?, ?)`,
		planID, taskID, dependsOn)
	if err != nil {
		return fmt.Errorf("store: insert dep: %w", err)
	}
	return nil
}

// wouldCreateCycle returns true if dep (task→depends_on) would make the
// graph cyclic. DFS from depends_on; if we can reach task, cycle.
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

// Ready returns tasks that are ready to run — every dependency is in a
// terminal-satisfied state (`done`, `skipped`, or `decomposed`). Result is
// ordered by created_at for stable test output.
func (s *Store) Ready(ctx context.Context, planID string) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, plan_id, description, status, parallel_group, sequence_ordinal,
		       COALESCE(acceptance_json,''),
		       COALESCE(claimed_by_session,''), COALESCE(claimed_by_worker_id,''),
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
		      AND tdep.status NOT IN ('done', 'skipped', 'decomposed')
		  )
		ORDER BY created_at
	`, planID)
	if err != nil {
		return nil, fmt.Errorf("store: query ready: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}

// ErrNoReadyTask indicates ClaimNextReady found nothing claimable.
var ErrNoReadyTask = errors.New("store: no ready task")

// ClaimNextReady is the atomic "claim the next ready task for this worker"
// operation. Returns the claimed task, or ErrNoReadyTask if none. Uses
// BEGIN (with _txlock=immediate at the DSN level) + UPDATE with a checked
// RowsAffected so two parallel workers never claim the same task.
func (s *Store) ClaimNextReady(ctx context.Context, planID, sessionID, workerID string) (*Task, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Find the first ready task: no unsatisfied deps, ordered by
	// sequence_ordinal (sequential steps run in author order) then
	// created_at (stable tie-break, e.g. within a parallel_group).
	selectQ := `
		SELECT id FROM tasks
		WHERE plan_id = ?
		  AND status = 'pending'
		  AND NOT EXISTS (
		    SELECT 1 FROM task_deps d
		     JOIN tasks tdep ON tdep.plan_id = d.plan_id AND tdep.id = d.depends_on
		    WHERE d.plan_id = tasks.plan_id
		      AND d.task_id = tasks.id
		      AND tdep.status NOT IN ('done', 'skipped', 'decomposed')
		  )
		ORDER BY
		  CASE WHEN sequence_ordinal IS NULL THEN 1 ELSE 0 END,
		  sequence_ordinal,
		  created_at
		LIMIT 1
	`
	var taskID string
	if err := tx.QueryRowContext(ctx, selectQ, planID).Scan(&taskID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoReadyTask
		}
		return nil, fmt.Errorf("store: select ready: %w", err)
	}

	// Atomic claim: set status=running, claimed_by_session,
	// claimed_by_worker_id.
	res, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'running',
		    claimed_by_session = ?,
		    claimed_by_worker_id = ?
		WHERE plan_id = ? AND id = ? AND status = 'pending'
	`, sessionID, workerID, planID, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: claim update: %w", err)
	}
	// Verify the claim actually landed. With _txlock=immediate this tx
	// holds the write lock, so a concurrent claimer cannot have flipped
	// the row out from under us between the SELECT and this UPDATE — but
	// checking RowsAffected is the correctness backstop that guarantees we
	// never return a task we did not claim (which would let two workers
	// run the same task).
	claimed, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("store: claim rows affected: %w", err)
	}
	if claimed == 0 {
		return nil, ErrNoReadyTask
	}

	// Emit event for audit log.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events(plan_id, task_id, kind, actor, stream, payload_json)
		VALUES (?, ?, 'task.claimed', ?, 'worker', ?)
	`, planID, taskID, sessionID, payloadJSON(EventPayload{})); err != nil {
		return nil, fmt.Errorf("store: log claim: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit claim: %w", err)
	}

	// Fetch the now-claimed row for the return value.
	return s.GetTask(ctx, planID, taskID)
}

// GetTask loads one task by (plan_id, id).
func (s *Store) GetTask(ctx context.Context, planID, id string) (*Task, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, plan_id, description, status, parallel_group, sequence_ordinal,
		       COALESCE(acceptance_json,''),
		       COALESCE(claimed_by_session,''), COALESCE(claimed_by_worker_id,''),
		       retry_count, reclaim_count, COALESCE(parent_task_id,''),
		       created_at, updated_at
		FROM tasks WHERE plan_id = ? AND id = ?
	`, planID, id)

	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("store: task %q not found in plan %q", id, planID)
		}
		return nil, fmt.Errorf("store: get task: %w", err)
	}
	return &t, nil
}

// ListTasks returns tasks for one plan, optionally filtered by status.
func (s *Store) ListTasks(ctx context.Context, planID string, statuses []TaskStatus) ([]Task, error) {
	query := `
		SELECT id, plan_id, description, status, parallel_group, sequence_ordinal,
		       COALESCE(acceptance_json,''),
		       COALESCE(claimed_by_session,''), COALESCE(claimed_by_worker_id,''),
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
		return nil, fmt.Errorf("store: list tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}

// ListTaskEvents returns the most recent events for one task first.
func (s *Store) ListTaskEvents(ctx context.Context, planID, taskID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(project_id,''), plan_id, task_id, kind, COALESCE(stream,''),
		       COALESCE(actor,''), COALESCE(payload_json,''), occurred_at
		FROM events
		WHERE plan_id = ? AND task_id = ?
		ORDER BY occurred_at DESC, id DESC
		LIMIT ?
	`, planID, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list task events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan task event: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// MarkDone transitions a running task to done, logs the event, and returns
// the set of newly-ready downstream tasks.
func (s *Store) MarkDone(ctx context.Context, planID, taskID, sessionID string, evidenceJSON string) ([]Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Owner-guarded like MarkFailed: guard on the reporting session STILL
	// owning the running task. Without `claimed_by_session = ?`, a completion
	// from a session whose claim the reaper already reclaimed and reassigned
	// to a new worker would mark the task done with the STALE session's
	// evidence and clear the NEW owner's live claim — double execution plus a
	// dropped completion, the exact race the failure-side guard closes.
	res, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = 'done',
		                 claimed_by_session = NULL,
		                 claimed_by_worker_id = NULL
		WHERE plan_id = ? AND id = ? AND status = 'running' AND claimed_by_session = ?
	`, planID, taskID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("store: rows affected: %w", err)
	}
	if n == 0 {
		// Either the task isn't running or it's owned by a different session
		// now (reclaimed + reassigned). Surface the same sentinel as
		// MarkFailed so the caller can treat a stale completion as a benign
		// no-op rather than a hard error.
		return nil, ErrTaskNotOwnedRunning
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events(plan_id, task_id, kind, actor, stream, payload_json)
		VALUES (?, ?, 'worker.completed', ?, 'worker', ?)
	`, planID, taskID, sessionID, jsonOrEmptyObject(evidenceJSON)); err != nil {
		return nil, fmt.Errorf("store: log done: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit: %w", err)
	}

	// Return the new ready set so callers can hand the next task to a
	// worker without a separate round-trip.
	return s.Ready(ctx, planID)
}

// ErrTaskNotOwnedRunning is returned by MarkFailed* when the task is no
// longer both running AND claimed by the reporting session — i.e. it was
// reclaimed by the reaper and (possibly) reassigned to another worker
// between dispatch and this failure report. It is a BENIGN outcome, not a
// hard error: the stale report is correctly dropped instead of stomping the
// current owner. Callers distinguish it via errors.Is and treat it as
// "someone else owns this now; nothing to do."
var ErrTaskNotOwnedRunning = errors.New("store: task not running under the reporting session (stale failure report)")

// MarkFailed transitions a running task, owned by sessionID, to failed or
// retries. See MarkFailedWithPayload.
func (s *Store) MarkFailed(ctx context.Context, planID, taskID, sessionID, reason string, maxRetries int) (retried bool, err error) {
	return s.MarkFailedWithPayload(ctx, planID, taskID, sessionID, EventPayload{Reason: reason}, maxRetries)
}

// MarkFailedWithPayload transitions a running task to failed or retries
// while preserving structured payload details in the event log.
//
// Both UPDATEs are guarded by `status = 'running' AND claimed_by_session =
// sessionID`, the same owner guard MarkDone and MarkBlocked carry. Without the
// owner guard a stale failure report from a worker whose claim the reaper
// already reclaimed (and possibly reassigned to a new worker) would flip the
// task back to pending / failed and clear the NEW owner's claim — double
// execution plus a dropped completion. When the guard matches nothing the
// task has moved on under a different session; MarkFailed returns
// ErrTaskNotOwnedRunning so the caller can drop the stale report rather than
// resurrect the task.
func (s *Store) MarkFailedWithPayload(ctx context.Context, planID, taskID, sessionID string, payload EventPayload, maxRetries int) (retried bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var retries int
	if err := tx.QueryRowContext(ctx,
		`SELECT retry_count FROM tasks WHERE plan_id = ? AND id = ? AND status = 'running' AND claimed_by_session = ?`,
		planID, taskID, sessionID).Scan(&retries); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, ErrTaskNotOwnedRunning
		}
		return false, fmt.Errorf("store: read retry_count: %w", err)
	}

	if retries+1 <= maxRetries {
		res, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET status = 'pending',
			    retry_count = retry_count + 1,
			    claimed_by_session = NULL,
			    claimed_by_worker_id = NULL
			WHERE plan_id = ? AND id = ? AND status = 'running' AND claimed_by_session = ?
		`, planID, taskID, sessionID)
		if err != nil {
			return false, fmt.Errorf("store: requeue: %w", err)
		}
		if n, err := res.RowsAffected(); err != nil {
			return false, fmt.Errorf("store: requeue rows affected: %w", err)
		} else if n == 0 {
			return false, ErrTaskNotOwnedRunning
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO events(plan_id, task_id, kind, actor, stream, payload_json)
			VALUES (?, ?, 'task.failed', ?, 'worker', ?)
		`, planID, taskID, sessionID, payloadJSON(payload)); err != nil {
			return false, fmt.Errorf("store: log failed: %w", err)
		}
		return true, tx.Commit()
	}

	// Out of retries.
	res, err := tx.ExecContext(ctx, `
		UPDATE tasks SET status = 'failed',
		                 claimed_by_session = NULL,
		                 claimed_by_worker_id = NULL
		WHERE plan_id = ? AND id = ? AND status = 'running' AND claimed_by_session = ?
	`, planID, taskID, sessionID)
	if err != nil {
		return false, fmt.Errorf("store: mark failed: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return false, fmt.Errorf("store: mark failed rows affected: %w", err)
	} else if n == 0 {
		return false, ErrTaskNotOwnedRunning
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events(plan_id, task_id, kind, actor, stream, payload_json)
		VALUES (?, ?, 'task.failed_terminal', ?, 'worker', ?)
	`, planID, taskID, sessionID, payloadJSON(payload)); err != nil {
		return false, fmt.Errorf("store: log failed terminal: %w", err)
	}
	return false, tx.Commit()
}

// ReleaseClaim requeues a running task owned by sessionID back to pending
// WITHOUT charging a retry, for SYSTEM-level aborts (e.g. a fan-out group
// whose later claim failed) as opposed to task-execution failures. Using
// MarkFailed here would penalize the retry budget for something the task
// never got a chance to attempt, and could terminally fail an otherwise-valid
// task after a few transient orchestrator hiccups. Owner-guarded like
// MarkFailed: a task no longer running under sessionID yields
// ErrTaskNotOwnedRunning (benign — someone else owns it now).
func (s *Store) ReleaseClaim(ctx context.Context, planID, taskID, sessionID, reason string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'pending',
		    claimed_by_session = NULL,
		    claimed_by_worker_id = NULL
		WHERE plan_id = ? AND id = ? AND status = 'running' AND claimed_by_session = ?
	`, planID, taskID, sessionID)
	if err != nil {
		return fmt.Errorf("store: release claim: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: release claim rows affected: %w", err)
	}
	if n == 0 {
		return ErrTaskNotOwnedRunning
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO events(plan_id, task_id, kind, actor, stream, payload_json)
		VALUES (?, ?, 'task.released', ?, 'orch', ?)
	`, planID, taskID, sessionID, payloadJSON(EventPayload{Reason: reason})); err != nil {
		return fmt.Errorf("store: log release: %w", err)
	}
	return nil
}

// MarkBlocked releases a running task into the blocked set so an operator
// can later requeue or otherwise intervene.
func (s *Store) MarkBlocked(ctx context.Context, planID, taskID, sessionID string, payload EventPayload) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Owner-guarded like MarkDone/MarkFailed: a stale context-end report from
	// a reclaimed+reassigned worker must not block the NEW owner's live task.
	res, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'blocked',
		    claimed_by_session = NULL,
		    claimed_by_worker_id = NULL
		WHERE plan_id = ? AND id = ? AND status = 'running' AND claimed_by_session = ?
	`, planID, taskID, sessionID)
	if err != nil {
		return fmt.Errorf("store: block task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: block task rows affected: %w", err)
	}
	if n == 0 {
		return ErrTaskNotOwnedRunning
	}

	kind := "task.blocked"
	if len(payload.NeedsContext) > 0 {
		kind = "task.context_requested"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events(plan_id, task_id, kind, actor, stream, payload_json)
		VALUES (?, ?, ?, ?, 'worker', ?)
	`, planID, taskID, kind, sessionID, payloadJSON(payload)); err != nil {
		return fmt.Errorf("store: log block: %w", err)
	}
	return tx.Commit()
}

func payloadJSON(payload EventPayload) string {
	if isZeroPayload(payload) {
		return "{}"
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func jsonOrEmptyObject(raw string) string {
	if raw == "" {
		return "{}"
	}
	return raw
}

func isZeroPayload(payload EventPayload) bool {
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

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(scanner taskScanner) (Task, error) {
	var t Task
	var createdStr, updatedStr string
	err := scanner.Scan(
		&t.ID, &t.PlanID, &t.Description, &t.Status, &t.ParallelGroup, &t.SequenceOrdinal,
		&t.AcceptanceJSON,
		&t.ClaimedBySession, &t.ClaimedByWorkerID,
		&t.RetryCount, &t.ReclaimCount, &t.ParentTaskID,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return Task{}, err
	}
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

// ApproveTask clears the approval gate on a task: it transitions a
// ready_pending_approval task to ready so dispatch can pick it up. Idempotent
// on the desired end state — approving a task that is already ready (or past
// it) returns found=true, changed=false rather than erroring. found=false (no
// error) when the task doesn't exist, so the caller can surface CodeNotFound.
func (s *Store) ApproveTask(ctx context.Context, planID, taskID string) (found, changed bool, err error) {
	var status string
	err = s.db.QueryRowContext(ctx,
		`SELECT status FROM tasks WHERE plan_id = ? AND id = ?`, planID, taskID,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("store: load task for approve: %w", err)
	}
	if status != string(TaskStatusReadyPendingApproval) {
		// Already approved / not awaiting approval — idempotent success.
		return true, false, nil
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE tasks SET status = 'ready'
		WHERE plan_id = ? AND id = ? AND status = 'ready_pending_approval'
	`, planID, taskID)
	if err != nil {
		return false, false, fmt.Errorf("store: approve task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, false, fmt.Errorf("store: approve rows affected: %w", err)
	}
	return true, n > 0, nil
}
