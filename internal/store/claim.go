package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	namedClaimBusyRetryWindow  = 5 * time.Second
	namedClaimBusyRetryBackoff = 10 * time.Millisecond
)

// ClaimTask atomically claims one NAMED dependency-ready task.
//
// It is the graph counterpart to ClaimNextReady, which picks whichever ready
// task its ORDER BY surfaces. That substitution forced the orchestrator to
// reconcile afterward — claiming a different task than the one it intended and
// then resolving it back to a plan step by id. With explicit edges the
// dispatcher knows which task it wants, so the claim must be exact: this never
// substitutes a different task, and returns ErrNoReadyTask when the named one
// is not claimable.
//
// The readiness predicate is deliberately identical to ClaimNextReady's, minus
// the ordering and LIMIT: same claimable statuses, same NOT EXISTS walk over
// task_deps. Two predicates that must agree but are written twice would drift,
// so any change here belongs in both.
func (s *Store) ClaimTask(
	ctx context.Context,
	planID, taskID, sessionID, workerID string,
) (*Task, error) {
	// SQLite admits one writer at a time. Serializing this supervisor's short
	// claim transactions in-process keeps a large ready wave from turning
	// ordinary queueing into SQLITE_BUSY timeouts. The database transaction is
	// still the cross-process correctness boundary; provider work runs outside
	// this gate and stays parallel.
	select {
	case s.claimGate <- struct{}{}:
		defer func() { <-s.claimGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	deadline := time.Now().Add(namedClaimBusyRetryWindow)
	for {
		task, err := s.claimTaskOnce(ctx, planID, taskID, sessionID, workerID)
		if err == nil || !isSQLiteBusy(err) || !time.Now().Before(deadline) {
			return task, err
		}
		timer := time.NewTimer(namedClaimBusyRetryBackoff)
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

// claimTaskOnce performs one complete immediate transaction. Only SQLITE_BUSY is
// retryable, and a retry must re-run the whole thing: once another writer
// commits, every readiness predicate has to be evaluated from a fresh
// transaction rather than reused.
func (s *Store) claimTaskOnce(
	ctx context.Context,
	planID, taskID, sessionID, workerID string,
) (*Task, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("store: begin named claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var selected string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM tasks
		WHERE plan_id = ? AND id = ?
		  AND status IN ('pending', 'ready')
		  AND NOT EXISTS (
		    SELECT 1 FROM task_deps d
		     JOIN tasks tdep ON tdep.plan_id = d.plan_id AND tdep.id = d.depends_on
		    WHERE d.plan_id = tasks.plan_id
		      AND d.task_id = tasks.id
		      AND tdep.status NOT IN ('done', 'skipped', 'decomposed')
		  )
	`, planID, taskID).Scan(&selected)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNoReadyTask
		}
		return nil, fmt.Errorf("store: select named ready task: %w", err)
	}

	// Admission, checked INSIDE the claim transaction. Readiness (the edge walk
	// above) says this task's dependencies are satisfied; it says nothing about
	// a peer that writes the same file. Checking here rather than in a separate
	// query is what makes it safe: two claimers would otherwise both observe no
	// conflict and both proceed, which is the exact race reservations exist to
	// prevent.
	if conflict, err := s.outputReservationConflict(ctx, tx, planID, selected); err != nil {
		return nil, err
	} else if conflict != "" {
		return nil, fmt.Errorf("%w: %q", ErrOutputReserved, conflict)
	}

	// The status guard mirrors the SELECT's claimable set. If it matched only
	// 'pending', an approved 'ready' task would pass the SELECT and then fail
	// the UPDATE, reporting ErrNoReadyTask and stranding every approved task.
	res, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'running',
		    claimed_by_session = ?,
		    claimed_by_worker_id = ?
		WHERE plan_id = ? AND id = ? AND status IN ('pending', 'ready')
	`, sessionID, workerID, planID, selected)
	if err != nil {
		return nil, fmt.Errorf("store: named claim update: %w", err)
	}
	// RowsAffected is the correctness backstop: it guarantees we never return a
	// task we did not actually claim, which would let two workers run one task.
	claimed, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("store: named claim rows affected: %w", err)
	}
	if claimed == 0 {
		return nil, ErrNoReadyTask
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO events(plan_id, task_id, kind, actor, stream, payload_json)
		VALUES (?, ?, 'task.claimed', ?, 'worker', ?)
	`, planID, selected, sessionID, payloadJSON(EventPayload{})); err != nil {
		return nil, fmt.Errorf("store: log named claim: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: commit named claim: %w", err)
	}
	return s.GetTask(ctx, planID, selected)
}
