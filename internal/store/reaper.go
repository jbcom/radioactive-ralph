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

	// Step 1: requeue tasks claimed by a worker whose heartbeat is stale,
	// OR already orphaned outright. A task is reclaimable if it is
	// 'running' and either:
	//
	//   (a) its claiming worker's last_heartbeat predates staleCutoff, or
	//   (b) claimed_by_worker_id is already NULL — the worker row is
	//       gone (e.g. workers.session_id ON DELETE CASCADE already ran
	//       from some OTHER path, such as a session being force-closed),
	//       so tasks.claimed_by_worker_id was cascaded to NULL but
	//       tasks.status was never independently transitioned back to
	//       'pending'. Without branch (b) such a task is invisible to
	//       this WHERE clause forever (claimed_by_worker_id IS NOT NULL
	//       excludes it) and stays wedged in 'running' with no claiming
	//       worker for good — exactly the permanently-stuck-task failure
	//       mode the reaper exists to prevent.
	//
	// SAFETY INVARIANT for branch (b): it reclaims WITHOUT a staleness
	// check, so it is only safe while NO code path can leave a task
	// 'running' with a NULL worker id *while that task's provider
	// subprocess is still executing* — otherwise (b) would re-dispatch a
	// live task and run it twice. The only producer of a NULL worker id on
	// a running task is a CASCADE from deleting a worker/session row, so
	// branch (b)'s safety rests entirely on NEVER deleting a worker/session
	// whose subprocess is still live. That is enforced two ways: step 2
	// below skips any session that still owns a fresh-heartbeat worker (the
	// load-bearing guard), and runWithHeartbeat now beats BOTH the worker
	// row and its session row so a live worker's session never goes stale
	// in the first place. (CloseSession is not a hazard: it runs only at
	// supervisor startup-failure with nothing claimed, or post-shutdown
	// after orch.Wait() has drained every in-flight dispatch goroutine.)
	// RETURNING names the rows this UPDATE touched, so each reclaim can emit an
	// event that says WHICH task was requeued and under which branch.
	//
	// A previous comment here claimed UPDATE...RETURNING was not portable on the
	// pinned modernc driver and settled for one summary row per pass
	// ({"reclaimed":N}). That was re-tested against modernc.org/sqlite v1.54.0
	// and it works, returning both the affected ids and their post-update
	// values. The summary row could not answer the only question an operator
	// actually has -- a task showing reclaim_count=2 was indistinguishable from
	// one whose worker crashed twice, and diagnosing the difference meant
	// reading watchdog source by hand.
	//
	// The reason must be captured BEFORE the UPDATE, not in its RETURNING
	// clause. RETURNING evaluates against the POST-update row, where
	// claimed_by_worker_id has just been set to NULL -- so a CASE on that column
	// reports 'orphaned_claim' for every reclaim, including the stale-heartbeat
	// ones. That mislabelled all reclaims until the test caught it.
	//
	// It cannot be recovered afterwards either: step 2 below may delete the
	// worker row, so a later lookup could not tell the branches apart at all.
	// Hence a SELECT against the same predicate, inside the same transaction.
	reasons := map[[2]string]string{}
	preRows, err := tx.QueryContext(ctx, `
		SELECT plan_id, id,
		       CASE WHEN claimed_by_worker_id IS NULL
		            THEN 'orphaned_claim' ELSE 'stale_heartbeat' END
		FROM tasks
		WHERE status = 'running'
		  AND (
		    claimed_by_worker_id IS NULL
		    OR claimed_by_worker_id IN (
		      SELECT id FROM workers WHERE last_heartbeat < ?
		    )
		  )
	`, staleCutoff)
	if err != nil {
		return 0, fmt.Errorf("store: classify reclaimable tasks: %w", err)
	}
	for preRows.Next() {
		var planID, taskID, reason string
		if err := preRows.Scan(&planID, &taskID, &reason); err != nil {
			_ = preRows.Close()
			return 0, fmt.Errorf("store: scan reclaimable task: %w", err)
		}
		reasons[[2]string{planID, taskID}] = reason
	}
	if err := preRows.Err(); err != nil {
		_ = preRows.Close()
		return 0, fmt.Errorf("store: iterate reclaimable tasks: %w", err)
	}
	if err := preRows.Close(); err != nil {
		return 0, fmt.Errorf("store: close reclaimable tasks: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		UPDATE tasks
		SET status = 'pending',
		    reclaim_count = reclaim_count + 1,
		    claimed_by_session = NULL,
		    claimed_by_worker_id = NULL
		WHERE status = 'running'
		  AND (
		    claimed_by_worker_id IS NULL
		    OR claimed_by_worker_id IN (
		      SELECT id FROM workers WHERE last_heartbeat < ?
		    )
		  )
		RETURNING plan_id, id, reclaim_count
	`, staleCutoff)
	if err != nil {
		return 0, fmt.Errorf("store: reclaim tasks: %w", err)
	}
	type reclaimedTask struct {
		planID, taskID, reason string
		count                  int
	}
	var touched []reclaimedTask
	for rows.Next() {
		var rt reclaimedTask
		if err := rows.Scan(&rt.planID, &rt.taskID, &rt.count); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("store: scan reclaimed task: %w", err)
		}
		rt.reason = reasons[[2]string{rt.planID, rt.taskID}]
		if rt.reason == "" {
			// The pre-classification and the UPDATE ran against the same
			// predicate in one transaction, so every updated row must have been
			// seen. Name the gap rather than emitting an event whose reason
			// field is silently empty.
			rt.reason = "unclassified"
		}
		touched = append(touched, rt)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("store: iterate reclaimed tasks: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("store: close reclaimed tasks: %w", err)
	}
	reclaimed = len(touched)

	// One event per reclaimed task, carrying plan_id and task_id so it lands in
	// the same project-scoped stream as task.claimed/done/failed and the
	// operator sees the reclaim beside the lifecycle it interrupted.
	for _, rt := range touched {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO events(plan_id, task_id, kind, stream, payload_json)
			VALUES (?, ?, 'task.reclaimed', 'service', ?)
		`, rt.planID, rt.taskID,
			fmt.Sprintf(`{"reason":%q,"reclaim_count":%d}`, rt.reason, rt.count)); err != nil {
			return 0, fmt.Errorf("store: log reclaim: %w", err)
		}
	}

	// Step 2: delete workers stale beyond the longer window. A worker with a
	// FRESH heartbeat is excluded even if its row is otherwise deletable —
	// deleting a live worker would cascade tasks.claimed_by_worker_id to NULL
	// and let step-1 branch (b) re-dispatch its still-running task (double
	// execution). last_heartbeat < deleteCutoff already implies the worker is
	// long dead, so this is belt-and-suspenders against the session path below.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM workers WHERE last_heartbeat < ?
	`, deleteCutoff); err != nil {
		return 0, fmt.Errorf("store: delete stale workers: %w", err)
	}

	// Delete sessions stale beyond the same longer window, BUT never a session
	// that still owns a live (fresh-heartbeat) worker. This is the load-bearing
	// guard for the never-double-execute invariant: a worker's own session row
	// (spawnWorkerRows) is not independently heartbeated — only the worker row
	// is (runWithHeartbeat) — so a provider turn running longer than
	// deleteCutoff (staleSessionMultiplier * staleAfter) would otherwise let
	// this DELETE reap the worker's stale session, CASCADE-delete the LIVE
	// worker (workers.session_id ON DELETE CASCADE), NULL out its running task's
	// claim, and hand that still-executing task to a second worker via step-1
	// branch (b). Excluding sessions with any non-stale worker closes that path
	// even though runWithHeartbeat also now beats the session directly.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE last_heartbeat < ?
		  AND id NOT IN (
		    SELECT session_id FROM workers WHERE last_heartbeat >= ?
		  )
	`, deleteCutoff, staleCutoff); err != nil {
		return 0, fmt.Errorf("store: delete stale sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("store: commit: %w", err)
	}
	return reclaimed, nil
}
