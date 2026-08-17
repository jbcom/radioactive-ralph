package store

import (
	"context"
	"fmt"
)

// HookTask identifies one currently running task assigned to the managed
// provider session carried in a generated hook environment. One native-fanout
// turn may own several tasks, so callers must verify every returned row.
type HookTask struct {
	PlanID   string
	TaskID   string
	WorkerID string
	Provider string
}

// RunningHookTasks returns every live task whose immutable execution metadata
// names sessionID. It never accepts a provider's own session identifier: only
// Ralph's random store session id crosses the hook boundary.
func (s *Store) RunningHookTasks(ctx context.Context, sessionID string) ([]HookTask, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("store: hook session id required")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.plan_id, t.id, COALESCE(t.claimed_by_worker_id, ''),
		       COALESCE(m.assigned_provider, '')
		FROM tasks t
		JOIN task_metadata m
		  ON m.plan_id = t.plan_id AND m.task_id = t.id
		WHERE t.status = 'running'
		  AND t.claimed_by_session = ?
		  AND m.assigned_session_id = ?
		ORDER BY t.plan_id, t.id
	`, sessionID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("store: list hook tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []HookTask
	for rows.Next() {
		var task HookTask
		if err := rows.Scan(&task.PlanID, &task.TaskID, &task.WorkerID, &task.Provider); err != nil {
			return nil, fmt.Errorf("store: scan hook task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate hook tasks: %w", err)
	}
	return tasks, nil
}
