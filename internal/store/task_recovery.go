package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrTaskNotRetryable means the task exists but is not in an operator-
// recoverable blocked state. Only capability and input admission failures are
// retryable through this API; running, completed, failed, and approval-gated
// tasks keep their existing lifecycle controls.
var ErrTaskNotRetryable = errors.New("store: task is not blocked on input or capability")

// RetryBlockedTask safely requeues a task after an operator has corrected its
// immutable input checkout or available provider pool. The transition is
// atomic and deliberately narrow: only blocked_input and blocked_capability
// become pending. Dispatch rechecks hashes, reservations, capabilities, and
// dependencies before a provider can run.
func (s *Store) RetryBlockedTask(
	ctx context.Context,
	planID, taskID string,
) (found, changed bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, false, fmt.Errorf("store: begin retry blocked task: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var status TaskStatus
	err = tx.QueryRowContext(ctx,
		`SELECT status FROM tasks WHERE plan_id = ? AND id = ?`,
		planID, taskID,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("store: load blocked task: %w", err)
	}
	if status != TaskStatusBlockedInput && status != TaskStatusBlockedCapability {
		return true, false, ErrTaskNotRetryable
	}

	res, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'pending', claimed_by_session = NULL, claimed_by_worker_id = NULL
		WHERE plan_id = ? AND id = ?
		  AND status IN ('blocked_input', 'blocked_capability')
	`, planID, taskID)
	if err != nil {
		return true, false, fmt.Errorf("store: retry blocked task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return true, false, fmt.Errorf("store: retry blocked rows affected: %w", err)
	}
	if n == 0 {
		return true, false, ErrTaskNotRetryable
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE task_metadata SET blocked_reason = NULL
		WHERE plan_id = ? AND task_id = ?
	`, planID, taskID); err != nil {
		return true, false, fmt.Errorf("store: clear blocked reason: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events(plan_id, task_id, kind, actor, stream, payload_json)
		VALUES (?, ?, 'task.requeued', 'operator', 'task',
		        '{"operator_action":"retry","retryable":true}')
	`, planID, taskID); err != nil {
		return true, false, fmt.Errorf("store: log blocked retry: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return true, false, fmt.Errorf("store: commit blocked retry: %w", err)
	}
	return true, true, nil
}
