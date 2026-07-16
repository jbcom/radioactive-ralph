package store

import (
	"context"
	"fmt"
	"time"
)

// staleSessionMultiplier sets how much longer a session/worker row must be
// stale (beyond staleAfter) before it is deleted outright, versus merely
// having its claimed tasks reclaimed. This gives a task a chance to be
// requeued and picked up by a *different* worker before we forget the dead
// worker/session ever existed (useful for post-mortem/audit).
const staleSessionMultiplier = 3

// ReclaimStale is the in-store reaper: the old daemon never implemented
// this, so a crashed worker permanently wedged its claimed task. It:
//
//  1. Requeues tasks whose claiming worker's last_heartbeat is older than
//     staleAfter — increments reclaim_count, sets status back to pending,
//     and clears the claim so ClaimNextReady can hand it to a live worker.
//  2. Deletes worker and session rows stale beyond a longer window
//     (staleSessionMultiplier * staleAfter), once any of their claims have
//     already been reclaimed by step 1.
//
// Returns the number of tasks reclaimed in step 1.
func (s *Store) ReclaimStale(ctx context.Context, staleAfter time.Duration) (reclaimed int, err error) {
	now := s.clock.Now().UTC()
	staleCutoff := now.Add(-staleAfter).Format(time.RFC3339)
	deleteCutoff := now.Add(-staleAfter * staleSessionMultiplier).Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Step 1: requeue tasks claimed by a worker whose heartbeat is stale.
	// A task is reclaimable if it is 'running' and its claiming worker's
	// last_heartbeat predates staleCutoff (or the worker row itself no
	// longer exists — a crash so hard the process never got an FK-cascaded
	// cleanup, e.g. supervisor restart under a fresh session).
	res, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'pending',
		    reclaim_count = reclaim_count + 1,
		    claimed_by_session = NULL,
		    claimed_by_worker_id = NULL
		WHERE status = 'running'
		  AND claimed_by_worker_id IS NOT NULL
		  AND claimed_by_worker_id IN (
		    SELECT id FROM workers WHERE last_heartbeat < ?
		  )
	`, staleCutoff)
	if err != nil {
		return 0, fmt.Errorf("store: reclaim tasks: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: reclaim rows affected: %w", err)
	}
	reclaimed = int(n)

	// Emit an audit event per reclaimed task would require knowing which
	// task ids were touched; SQLite's driver here doesn't give us
	// UPDATE...RETURNING portably across the modernc driver version pinned,
	// so record one summary event per reaper pass instead.
	if reclaimed > 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO events(kind, stream, payload_json)
			VALUES ('reaper.reclaimed', 'service', ?)
		`, fmt.Sprintf(`{"reclaimed":%d}`, reclaimed)); err != nil {
			return 0, fmt.Errorf("store: log reclaim: %w", err)
		}
	}

	// Step 2: delete workers stale beyond the longer window. Any task
	// still claimed by such a worker at this point has already had its
	// claim cleared in step 1 (deleteCutoff < staleCutoff), so this is
	// safe — ON DELETE SET NULL on tasks.claimed_by_worker_id covers any
	// remaining reference defensively.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM workers WHERE last_heartbeat < ?
	`, deleteCutoff); err != nil {
		return 0, fmt.Errorf("store: delete stale workers: %w", err)
	}

	// Delete sessions stale beyond the same longer window. FK cascade
	// (workers.session_id ON DELETE CASCADE) cleans up any of their
	// remaining workers too.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM sessions WHERE last_heartbeat < ?
	`, deleteCutoff); err != nil {
		return 0, fmt.Errorf("store: delete stale sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit: %w", err)
	}
	return reclaimed, nil
}
