package plandag

import (
	"context"
	"fmt"
)

// TaskDepsResult captures both directions of a task's DAG edges:
// the tasks this one depends on, and the tasks that depend on this
// one. Each side is returned as a list of task IDs in ascending
// lexical order so TUI drilldowns render deterministically.
type TaskDepsResult struct {
	DependsOn  []string // tasks this task waits for
	DependedBy []string // tasks that wait for this task
}

// TaskDeps returns the up-and-down neighborhood for (planID, taskID)
// in a single round trip. Used by the TUI task-detail view.
func (s *Store) TaskDeps(ctx context.Context, planID, taskID string) (TaskDepsResult, error) {
	var out TaskDepsResult

	depsRows, err := s.db.QueryContext(ctx, `
		SELECT depends_on FROM task_deps
		 WHERE plan_id = ? AND task_id = ?
		 ORDER BY depends_on
	`, planID, taskID)
	if err != nil {
		return out, fmt.Errorf("plandag: query depends_on: %w", err)
	}
	for depsRows.Next() {
		var id string
		if err := depsRows.Scan(&id); err != nil {
			_ = depsRows.Close()
			return out, err
		}
		out.DependsOn = append(out.DependsOn, id)
	}
	_ = depsRows.Close()
	if err := depsRows.Err(); err != nil {
		return out, err
	}

	byRows, err := s.db.QueryContext(ctx, `
		SELECT task_id FROM task_deps
		 WHERE plan_id = ? AND depends_on = ?
		 ORDER BY task_id
	`, planID, taskID)
	if err != nil {
		return out, fmt.Errorf("plandag: query depended_by: %w", err)
	}
	for byRows.Next() {
		var id string
		if err := byRows.Scan(&id); err != nil {
			_ = byRows.Close()
			return out, err
		}
		out.DependedBy = append(out.DependedBy, id)
	}
	_ = byRows.Close()
	return out, byRows.Err()
}
