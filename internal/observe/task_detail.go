package observe

import (
	"context"
	"fmt"
)

// TaskDescriptionsQuery names one plan whose task descriptions to read.
type TaskDescriptionsQuery struct {
	ProjectID string `json:"project_id"`
	PlanID    string `json:"plan_id"`
	// TaskIDs bounds the read to the tasks the caller is actually rendering.
	// Without it a plan larger than one snapshot page would scan every
	// description on every refresh.
	TaskIDs []string `json:"task_ids"`
}

// TaskDescriptions carries author-written task labels for one plan.
//
// These are deliberately NOT part of Snapshot. A description is plan-author
// free text that can contain filesystem paths or other sensitive strings, and
// Snapshot is the always-on bulk surface that every client polls; emitting
// descriptions there would leak them to every observer for every task. Issue
// #204 forbids that for *default* DTOs while permitting exposure for legitimate
// follow-up, so descriptions live behind this explicit, plan-scoped opt-in that
// only the human-facing views request.
//
// Plan-scoped rather than per-task on purpose: a list view needs every visible
// label, and one query per task would cost N round trips inside a single
// refresh budget.
type TaskDescriptions struct {
	PlanID string            `json:"plan_id"`
	ByTask map[string]string `json:"by_task"`
}

// TaskDescriptions reads one plan's task labels, project-scoped so a caller
// cannot read across projects by guessing ids.
func (s *Service) TaskDescriptions(
	ctx context.Context,
	q TaskDescriptionsQuery,
) (TaskDescriptions, error) {
	byTask, err := s.reader.ListOperatorTaskDescriptions(ctx, q.ProjectID, q.PlanID, q.TaskIDs)
	if err != nil {
		return TaskDescriptions{}, fmt.Errorf("observe: task descriptions: %w", err)
	}
	return TaskDescriptions{PlanID: q.PlanID, ByTask: byTask}, nil
}
