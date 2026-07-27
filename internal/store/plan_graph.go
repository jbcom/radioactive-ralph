package store

import (
	"context"
	"fmt"
)

// GraphTaskSpec is one node in a plan graph: the task plus the ids it depends
// on. DependsOn names other tasks in the SAME plan.
type GraphTaskSpec struct {
	CreateTaskOpts
	DependsOn []string
	// GroupPath is the task's leaf-group identity, persisted so dispatch can
	// partition a ready wave without re-parsing the plan markdown.
	GroupPath string
	// TeamPath groups tasks in the operator views. Empty is fine.
	TeamPath string
	// MetadataJSON is the raw ralph-task block, or "{}" when the step carried
	// no annotation.
	MetadataJSON string
}

// CreatePlanGraphOpts is one plan and its complete task graph.
type CreatePlanGraphOpts struct {
	CreatePlanOpts
	Tasks []GraphTaskSpec
	// Activate marks the plan active on success, so a freshly imported plan is
	// eligible for dispatch without a second call that could fail on its own.
	Activate bool
}

// CreatePlanGraph writes a plan, its tasks, their metadata, and every edge in
// ONE transaction.
//
// Atomicity is the reason this exists rather than a caller looping over
// CreatePlan/CreateTask/AddDep. Those autocommit individually, so a mid-import
// failure or a cancelled context would leave a draft plan plus however many
// nodes happened to land — and the retry would then hit ErrDuplicateSlug rather
// than completing, leaving a plan permanently undispatchable. That directly
// contradicts the fail-closed ingress guarantee ValidateForImport provides.
//
// Every write goes through the same *On helper the public single-shot methods
// use, so there is one implementation of each statement. In particular edges go
// through addDepOn, which runs its cycle check on this transaction and
// therefore sees the edges written moments earlier by this same import.
func (s *Store) CreatePlanGraph(ctx context.Context, o CreatePlanGraphOpts) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("store: begin plan graph: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	planID, err := s.createPlanOn(ctx, tx, o.CreatePlanOpts)
	if err != nil {
		return "", err
	}

	for _, task := range o.Tasks {
		spec := task.CreateTaskOpts
		spec.PlanID = planID
		if err := s.createTaskOn(ctx, tx, spec); err != nil {
			return "", err
		}
		if task.GroupPath != "" {
			metadata := task.MetadataJSON
			if metadata == "" {
				metadata = "{}"
			}
			if err := s.putTaskMetadataOn(
				ctx, tx, planID, spec.ID, task.GroupPath, task.TeamPath, metadata,
			); err != nil {
				return "", err
			}
		}
	}

	// Edges go last so every referenced task already exists — the FK on
	// task_deps would otherwise reject a forward reference, and a plan is
	// perfectly entitled to declare `after` against a step written below it.
	for _, task := range o.Tasks {
		for _, dep := range task.DependsOn {
			if err := s.addDepOn(ctx, tx, planID, task.ID, dep); err != nil {
				return "", err
			}
		}
	}

	if o.Activate {
		if err := s.setPlanStatusOn(ctx, tx, planID, PlanStatusActive); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("store: commit plan graph: %w", err)
	}
	return planID, nil
}
