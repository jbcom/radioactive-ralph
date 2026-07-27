package store

import (
	"context"
	"fmt"
)

// ReadyPartition is one dispatchable wave slice: the tasks that are ready RIGHT
// NOW and share a leaf group.
//
// Dispatch cannot treat "several tasks are ready" as "several tasks may be
// delegated together". Native fan-out hands a whole partition to ONE provider
// under one group heading, so the unit of fan-out is the leaf group, not the
// ready set. Two tasks from different groups being simultaneously ready is the
// normal case for a DAG and says nothing about whether one worker may own both.
type ReadyPartition struct {
	// GroupPath is the persisted leaf-group identity shared by every task in
	// Tasks (a dotted StepRef path such as "0.2"). Empty for tasks created
	// without a task_metadata row.
	GroupPath string
	Tasks     []Task
}

// ReadyPartitions returns the currently-ready tasks for planID, grouped by
// their persisted group_path and ordered deterministically.
//
// Readiness is the SAME NOT EXISTS walk over task_deps that Ready and
// ClaimNextReady use — this adds partitioning on top of it, it does not
// introduce a second notion of ready. The join to task_metadata is a LEFT join
// on purpose: a task materialized by the plain CreateTask path has no metadata
// row, and dropping it here would make its plan silently unrunnable.
//
// Partitions come back in group-path order, tasks within a partition in the
// same sequence_ordinal/created_at order ClaimNextReady picks them, so a caller
// that dispatches partition-by-partition reproduces author order.
func (s *Store) ReadyPartitions(ctx context.Context, planID string) ([]ReadyPartition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.plan_id, t.description, t.status, t.parallel_group,
		       t.sequence_ordinal, COALESCE(t.acceptance_json,''),
		       COALESCE(t.claimed_by_session,''), COALESCE(t.claimed_by_worker_id,''),
		       t.retry_count, t.reclaim_count, COALESCE(t.parent_task_id,''),
		       t.created_at, t.updated_at,
		       COALESCE(m.group_path,'')
		FROM tasks t
		LEFT JOIN task_metadata m
		       ON m.plan_id = t.plan_id AND m.task_id = t.id
		WHERE t.plan_id = ?
		  AND t.status IN ('pending', 'ready')
		  AND NOT EXISTS (
		    SELECT 1 FROM task_deps d
		     JOIN tasks tdep ON tdep.plan_id = d.plan_id AND tdep.id = d.depends_on
		    WHERE d.plan_id = t.plan_id
		      AND d.task_id = t.id
		      AND tdep.status NOT IN ('done', 'skipped', 'decomposed')
		  )
		ORDER BY
		  COALESCE(m.group_path,''),
		  CASE WHEN t.sequence_ordinal IS NULL THEN 1 ELSE 0 END,
		  t.sequence_ordinal,
		  t.created_at
	`, planID)
	if err != nil {
		return nil, fmt.Errorf("store: query ready partitions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var parts []ReadyPartition
	for rows.Next() {
		var (
			t         Task
			groupPath string
			status    string
		)
		if err := rows.Scan(
			&t.ID, &t.PlanID, &t.Description, &status, &t.ParallelGroup,
			&t.SequenceOrdinal, &t.AcceptanceJSON,
			&t.ClaimedBySession, &t.ClaimedByWorkerID,
			&t.RetryCount, &t.ReclaimCount, &t.ParentTaskID,
			&t.CreatedAt, &t.UpdatedAt,
			&groupPath,
		); err != nil {
			return nil, fmt.Errorf("store: scan ready partition row: %w", err)
		}
		t.Status = TaskStatus(status)

		// Rows arrive grouped by group_path, so a new value always starts a new
		// partition.
		if len(parts) == 0 || parts[len(parts)-1].GroupPath != groupPath {
			parts = append(parts, ReadyPartition{GroupPath: groupPath})
		}
		last := &parts[len(parts)-1]
		last.Tasks = append(last.Tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate ready partitions: %w", err)
	}
	return parts, nil
}
