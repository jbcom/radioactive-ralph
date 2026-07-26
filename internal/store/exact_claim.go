package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ErrOutputReserved reports an exclusive output currently owned by another
// running task in the same project.
var ErrOutputReserved = errors.New("store: exclusive output reserved")

const (
	exactClaimBusyRetryWindow  = 5 * time.Second
	exactClaimBusyRetryBackoff = 10 * time.Millisecond
)

// ClaimReadyTask atomically claims one named dependency-ready task. It is the
// explicit-DAG counterpart to ClaimNextReady and never substitutes a different
// ready task.
func (s *Store) ClaimReadyTask(
	ctx context.Context,
	planID, taskID, sessionID, workerID string,
) (*Task, error) {
	// SQLite admits one writer at a time. Serialize this supervisor's short
	// exact-claim transactions before they enter the driver so a large ready
	// wave does not turn ordinary in-process queueing into SQLITE_BUSY timeouts.
	// The database transaction remains the cross-process correctness boundary;
	// running provider work is outside this mutex and remains parallel.
	select {
	case s.exactClaimGate <- struct{}{}:
		defer func() { <-s.exactClaimGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	deadline := time.Now().Add(exactClaimBusyRetryWindow)
	for {
		task, err := s.claimReadyTaskOnce(ctx, planID, taskID, sessionID, workerID)
		if err == nil || !isSQLiteBusy(err) || !time.Now().Before(deadline) {
			return task, err
		}
		timer := time.NewTimer(exactClaimBusyRetryBackoff)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// claimReadyTaskOnce performs one complete immediate transaction. A caller
// retries only SQLITE_BUSY: after another writer commits, every readiness and
// reservation predicate must be evaluated again from a fresh transaction.
func (s *Store) claimReadyTaskOnce(
	ctx context.Context,
	planID, taskID, sessionID, workerID string,
) (*Task, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("store: begin exact claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var selected string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM tasks
		WHERE plan_id = ? AND id = ? AND status IN ('pending', 'ready')
		  AND NOT EXISTS (
		    SELECT 1 FROM task_deps d
		    JOIN tasks dependency
		      ON dependency.plan_id = d.plan_id AND dependency.id = d.depends_on
		    WHERE d.plan_id = tasks.plan_id AND d.task_id = tasks.id
		      AND dependency.status NOT IN ('done', 'skipped', 'decomposed')
		  )
	`, planID, taskID).Scan(&selected)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoReadyTask
		}
		return nil, fmt.Errorf("store: select exact ready task: %w", err)
	}
	if err := ensureOutputsAvailable(ctx, tx, planID, taskID); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'running', claimed_by_session = ?, claimed_by_worker_id = ?
		WHERE plan_id = ? AND id = ? AND status IN ('pending', 'ready')
	`, sessionID, workerID, planID, selected)
	if err != nil {
		return nil, fmt.Errorf("store: exact claim update: %w", err)
	}
	if count, err := res.RowsAffected(); err != nil {
		return nil, fmt.Errorf("store: exact claim rows affected: %w", err)
	} else if count == 0 {
		return nil, ErrNoReadyTask
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events(plan_id, task_id, kind, actor, stream, payload_json)
		VALUES (?, ?, 'task.claimed', ?, 'worker', ?)
	`, planID, selected, sessionID, payloadJSON(EventPayload{})); err != nil {
		return nil, fmt.Errorf("store: log exact claim: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit exact claim: %w", err)
	}
	return s.GetTask(ctx, planID, selected)
}

func ensureOutputsAvailable(ctx context.Context, tx *sql.Tx, planID, taskID string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT requested.path, active.path, active.task_id, 'write/write'
		FROM task_output_reservations requested
		JOIN plans requested_plan ON requested_plan.id = requested.plan_id
		JOIN plans active_plan ON active_plan.project_id = requested_plan.project_id
		JOIN task_output_reservations active ON active.plan_id = active_plan.id
		JOIN tasks active_task
		  ON active_task.plan_id = active.plan_id AND active_task.id = active.task_id
		WHERE requested.plan_id = ? AND requested.task_id = ?
		  AND active_task.status = 'running'
		  AND NOT (active.plan_id = requested.plan_id AND active.task_id = requested.task_id)

		UNION ALL

		SELECT requested.path, active.path, active.task_id, 'write/read'
		FROM task_output_reservations requested
		JOIN plans requested_plan ON requested_plan.id = requested.plan_id
		JOIN plans active_plan ON active_plan.project_id = requested_plan.project_id
		JOIN task_input_reservations active ON active.plan_id = active_plan.id
		JOIN tasks active_task
		  ON active_task.plan_id = active.plan_id AND active_task.id = active.task_id
		WHERE requested.plan_id = ? AND requested.task_id = ?
		  AND active_task.status = 'running'
		  AND NOT (active.plan_id = requested.plan_id AND active.task_id = requested.task_id)

		UNION ALL

		SELECT requested.path, active.path, active.task_id, 'read/write'
		FROM task_input_reservations requested
		JOIN plans requested_plan ON requested_plan.id = requested.plan_id
		JOIN plans active_plan ON active_plan.project_id = requested_plan.project_id
		JOIN task_output_reservations active ON active.plan_id = active_plan.id
		JOIN tasks active_task
		  ON active_task.plan_id = active.plan_id AND active_task.id = active.task_id
		WHERE requested.plan_id = ? AND requested.task_id = ?
		  AND active_task.status = 'running'
		  AND NOT (active.plan_id = requested.plan_id AND active.task_id = requested.task_id)
	`, planID, taskID, planID, taskID, planID, taskID)
	if err != nil {
		return fmt.Errorf("store: inspect output reservations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var requested, active, owner, conflictKind string
		if err := rows.Scan(&requested, &active, &owner, &conflictKind); err != nil {
			return fmt.Errorf("store: scan output reservation: %w", err)
		}
		if reservedPathsOverlap(requested, active) {
			return fmt.Errorf(
				"%w: task %s path %s overlaps running task %s path %s (%s)",
				ErrOutputReserved, taskID, requested, owner, active, conflictKind,
			)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: iterate output reservations: %w", err)
	}
	return nil
}

func reservedPathsOverlap(left, right string) bool {
	left = strings.ToLower(filepath.ToSlash(filepath.Clean(left)))
	right = strings.ToLower(filepath.ToSlash(filepath.Clean(right)))
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}
