package store

import (
	"context"
	"fmt"
	"time"
)

// HookVerificationState is the finite durable Stop-check lifecycle.
type HookVerificationState string

const (
	// HookVerificationPending means supervisor-owned verification is running.
	HookVerificationPending HookVerificationState = "pending"
	// HookVerificationPassed means the last independent check passed.
	HookVerificationPassed HookVerificationState = "passed"
	// HookVerificationFailed means the last independent check completed false.
	HookVerificationFailed HookVerificationState = "failed"
)

// HookVerificationKey identifies one task verdict within a managed session.
type HookVerificationKey struct {
	PlanID string
	TaskID string
}

// HookVerificationStates returns finite verdicts for one Ralph session. Raw
// provider data and diagnostics are structurally absent from the table.
func (s *Store) HookVerificationStates(
	ctx context.Context,
	sessionID string,
) (map[HookVerificationKey]HookVerificationState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT plan_id, task_id, state
		FROM hook_verifications WHERE session_id = ?
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: list hook verification states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	states := make(map[HookVerificationKey]HookVerificationState)
	for rows.Next() {
		var key HookVerificationKey
		var state HookVerificationState
		if err := rows.Scan(&key.PlanID, &key.TaskID, &state); err != nil {
			return nil, fmt.Errorf("store: scan hook verification state: %w", err)
		}
		states[key] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate hook verification states: %w", err)
	}
	return states, nil
}

// SetHookVerificationPending creates or resets one verification attempt. The
// live owner checks prevent a stale/reclaimed session from creating evidence.
func (s *Store) SetHookVerificationPending(
	ctx context.Context,
	sessionID, planID, taskID string,
) (bool, error) {
	now := s.clock.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO hook_verifications(session_id, plan_id, task_id, state, updated_at)
		SELECT ?, ?, ?, 'pending', ?
		WHERE EXISTS (
		  SELECT 1 FROM tasks t
		  JOIN task_metadata m ON m.plan_id = t.plan_id AND m.task_id = t.id
		  WHERE t.plan_id = ? AND t.id = ? AND t.status = 'running'
		    AND t.claimed_by_session = ? AND m.assigned_session_id = ?
		)
		ON CONFLICT(session_id, plan_id, task_id)
		DO UPDATE SET state = 'pending', updated_at = excluded.updated_at
	`, sessionID, planID, taskID, now, planID, taskID, sessionID, sessionID)
	if err != nil {
		return false, fmt.Errorf("store: set hook verification pending: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: inspect hook verification pending result: %w", err)
	}
	return affected == 1, nil
}

// SetHookVerificationResult records only a finite verdict and only while the
// same session still owns the running task. A stale async result is discarded.
func (s *Store) SetHookVerificationResult(
	ctx context.Context,
	sessionID, planID, taskID string,
	passed bool,
) error {
	state := HookVerificationFailed
	if passed {
		state = HookVerificationPassed
	}
	now := s.clock.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		UPDATE hook_verifications
		SET state = ?, updated_at = ?
		WHERE session_id = ? AND plan_id = ? AND task_id = ?
		  AND EXISTS (
		    SELECT 1 FROM tasks t
		    JOIN task_metadata m ON m.plan_id = t.plan_id AND m.task_id = t.id
		    WHERE t.plan_id = ? AND t.id = ? AND t.status = 'running'
		      AND t.claimed_by_session = ? AND m.assigned_session_id = ?
		  )
	`, state, now, sessionID, planID, taskID, planID, taskID, sessionID, sessionID)
	if err != nil {
		return fmt.Errorf("store: set hook verification result: %w", err)
	}
	return nil
}

// InvalidateHookVerifications drops cached verdicts after observable progress.
// The next Stop must independently verify the new checkout state.
func (s *Store) InvalidateHookVerifications(ctx context.Context, sessionID string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM hook_verifications WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("store: invalidate hook verifications: %w", err)
	}
	return nil
}

// ClearHookVerification removes one inconclusive attempt so a later Stop can
// retry it. It is distinct from a genuine failed acceptance verdict.
func (s *Store) ClearHookVerification(
	ctx context.Context,
	sessionID, planID, taskID string,
) error {
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM hook_verifications
		WHERE session_id = ? AND plan_id = ? AND task_id = ?
	`, sessionID, planID, taskID); err != nil {
		return fmt.Errorf("store: clear hook verification: %w", err)
	}
	return nil
}
