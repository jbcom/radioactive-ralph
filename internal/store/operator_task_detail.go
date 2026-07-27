package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// OperatorTaskDetail is the OPT-IN, single-task view. It carries the one field
// the bulk snapshot deliberately withholds: the task's author-written
// description.
//
// Why this is a separate call rather than a field on OperatorTask: Description
// is orch's step.Text — free text the plan author controls, which can contain
// filesystem paths, URLs, or anything else they typed. Putting it in the
// always-on snapshot would emit it for every task in every list to every
// observer, which is what the content-safety tests exist to prevent. Issue #204
// forbids that for *default* DTOs while allowing exposure for legitimate
// follow-up, so it is available here: one named task, requested deliberately,
// for an operator who has already drilled into it.
type OperatorTaskDetail struct {
	PlanID      string `json:"plan_id"`
	ID          string `json:"id"`
	Description string `json:"description"`
}

// ErrOperatorTaskDetailNotFound reports a detail read for a task that does not
// exist within the requested project.
var ErrOperatorTaskDetailNotFound = errors.New("store: operator task detail not found")

// ListOperatorTaskDescriptions returns task id -> description for one plan, in
// ONE query.
//
// The per-task ReadOperatorTaskDetail exists for a single drilled-in task; a
// list view must not call it in a loop. Each call is a separate round trip (and
// over IPC, a separate socket dial), so rendering an N-task page would cost N
// round trips inside one refresh budget — the same per-item-vs-per-page mistake
// ListTaskGroupPaths avoids on the dispatch side.
func (s *Store) ListOperatorTaskDescriptions(
	ctx context.Context,
	projectID, planID string,
) (map[string]string, error) {
	if projectID == "" || planID == "" {
		return nil, fmt.Errorf("store: operator task descriptions require project and plan ids")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.description
		FROM tasks t
		JOIN plans p ON p.id = t.plan_id
		WHERE p.project_id = ? AND t.plan_id = ?
	`, projectID, planID)
	if err != nil {
		return nil, fmt.Errorf("store: list operator task descriptions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var id, description string
		if err := rows.Scan(&id, &description); err != nil {
			return nil, fmt.Errorf("store: scan operator task description: %w", err)
		}
		out[id] = description
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate operator task descriptions: %w", err)
	}
	return out, nil
}

// ReadOperatorTaskDetail returns one task's description, scoped to a project so
// a caller cannot read across projects by guessing ids.
func (s *Store) ReadOperatorTaskDetail(
	ctx context.Context,
	projectID, planID, taskID string,
) (OperatorTaskDetail, error) {
	if projectID == "" || planID == "" || taskID == "" {
		return OperatorTaskDetail{}, fmt.Errorf(
			"store: operator task detail requires project, plan, and task ids")
	}
	var detail OperatorTaskDetail
	err := s.db.QueryRowContext(ctx, `
		SELECT t.plan_id, t.id, t.description
		FROM tasks t
		JOIN plans p ON p.id = t.plan_id
		WHERE p.project_id = ? AND t.plan_id = ? AND t.id = ?
	`, projectID, planID, taskID).Scan(&detail.PlanID, &detail.ID, &detail.Description)
	if errors.Is(err, sql.ErrNoRows) {
		return OperatorTaskDetail{}, fmt.Errorf("%w: %s/%s", ErrOperatorTaskDetailNotFound, planID, taskID)
	}
	if err != nil {
		return OperatorTaskDetail{}, fmt.Errorf("store: read operator task detail: %w", err)
	}
	return detail, nil
}
