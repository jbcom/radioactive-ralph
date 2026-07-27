package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
)

// ErrOutputReserved reports a claim refused because another RUNNING task in the
// same plan has declared an exclusive write to one of this task's output paths.
var ErrOutputReserved = errors.New("store: a running task already reserved this output path")

// ReserveTaskOutput records that taskID writes path exclusively.
//
// Reservations exist because readiness and admission are different questions.
// The dependency graph answers "are this task's predecessors done"; it says
// nothing about two independent tasks that happen to write the same file. Those
// tasks are legitimately ready at the same instant and must still not run
// concurrently — an edge between them would be a lie (neither consumes the
// other's result) and would also serialize them permanently rather than only
// while one is running.
func (s *Store) ReserveTaskOutput(ctx context.Context, planID, taskID, path, mode string) error {
	if path == "" {
		return fmt.Errorf("store: output path required for task %s", taskID)
	}
	if mode == "" {
		mode = "exclusive"
	}
	return s.reserveTaskOutputOn(ctx, s.db, planID, taskID, path, mode)
}

// reserveTaskOutputOn is the single implementation, usable standalone or inside
// a transaction (see execer).
func (s *Store) reserveTaskOutputOn(
	ctx context.Context, ex execer, planID, taskID, path, mode string,
) error {
	path = canonicalReservationPath(path)
	if path == "" {
		return fmt.Errorf("store: output path required for task %s", taskID)
	}
	if mode == "" {
		mode = "exclusive"
	}
	if _, err := ex.ExecContext(ctx, `
		INSERT INTO task_output_reservations(plan_id, task_id, path, mode)
		VALUES (?, ?, ?, ?)
	`, planID, taskID, path, mode); err != nil {
		return fmt.Errorf("store: reserve output %q for task %s: %w", path, taskID, err)
	}
	return nil
}

// ReserveTaskInput records that taskID reads path, pinned to sha256 when the
// plan declared a hash. An empty hash means "declared but unpinned".
func (s *Store) ReserveTaskInput(ctx context.Context, planID, taskID, path, sha256 string) error {
	if path == "" {
		return fmt.Errorf("store: input path required for task %s", taskID)
	}
	return s.reserveTaskInputOn(ctx, s.db, planID, taskID, path, sha256)
}

// reserveTaskInputOn is the single implementation, usable standalone or inside
// a transaction (see execer).
func (s *Store) reserveTaskInputOn(
	ctx context.Context, ex execer, planID, taskID, path, sha256 string,
) error {
	path = canonicalReservationPath(path)
	if path == "" {
		return fmt.Errorf("store: input path required for task %s", taskID)
	}
	if _, err := ex.ExecContext(ctx, `
		INSERT INTO task_input_reservations(plan_id, task_id, path, sha256)
		VALUES (?, ?, ?, ?)
	`, planID, taskID, path, sha256); err != nil {
		return fmt.Errorf("store: reserve input %q for task %s: %w", path, taskID, err)
	}
	return nil
}

// outputReservationConflict reports whether any of taskID's declared output
// paths is also declared by a task that is currently RUNNING.
//
// Scoped to running tasks on purpose: a reservation is a lock held for the
// duration of a run, not a permanent assignment of a path to a task. A finished
// holder must not block its peer forever.
//
// Runs on the caller's transaction so the check and the claim are atomic — a
// separate query would let two claimers both observe no conflict and both
// proceed, which is the exact race the reservation exists to prevent.
func (s *Store) outputReservationConflict(
	ctx context.Context, ex execer, planID, taskID string,
) (string, error) {
	var conflictPath string
	// Scoped to the PROJECT, not the plan. A reservation protects a filesystem
	// path in the project checkout, and the supervisor dispatches every active
	// plan concurrently — so a plan-scoped check let two plans' workers write
	// the same path at the same time, defeating the exclusivity outright.
	//
	// Not scoped WIDER than the project either: separate projects are separate
	// checkouts, so the same relative path names a different file and must not
	// conflict.
	err := ex.QueryRowContext(ctx, `
		SELECT mine.path
		FROM task_output_reservations mine
		JOIN plans mp        ON mp.id = mine.plan_id
		JOIN task_output_reservations theirs
		  ON theirs.path = mine.path
		 AND NOT (theirs.plan_id = mine.plan_id AND theirs.task_id = mine.task_id)
		JOIN plans tp        ON tp.id = theirs.plan_id AND tp.project_id = mp.project_id
		JOIN tasks holder
		  ON holder.plan_id = theirs.plan_id
		 AND holder.id      = theirs.task_id
		WHERE mine.plan_id = ?
		  AND mine.task_id = ?
		  AND holder.status = 'running'
		LIMIT 1
	`, planID, taskID).Scan(&conflictPath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("store: check output reservations for %s: %w", taskID, err)
	}
	return conflictPath, nil
}

// canonicalReservationPath normalizes a declared project-relative path so two
// spellings of the same file reserve the same string.
//
// The conflict query compares path TEXT, so without this "build/out.txt" and
// "build/./out.txt" name one file but reserve two different rows — both claims
// succeed and both workers write it, defeating exclusivity for no better reason
// than how the plan happened to be typed.
//
// Lexical only, and deliberately so: this runs inside the claim transaction's
// hot path and must not touch the filesystem. Symlink-level aliasing is a
// separate concern handled by the orchestrator's containment check, which
// resolves against the real checkout.
func canonicalReservationPath(path string) string {
	if path == "" {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." {
		return ""
	}
	return cleaned
}
