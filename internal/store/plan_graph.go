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
	// Inputs and Outputs are the task's declared filesystem surface, persisted
	// as reservations so the claim path can refuse to run two tasks that write
	// the same exclusive path concurrently. Declared paths are project-relative.
	Inputs  []TaskInputSpec
	Outputs []TaskOutputSpec
}

// TaskInputSpec is one declared input, optionally pinned to a content hash.
type TaskInputSpec struct {
	Path   string
	SHA256 string
}

// TaskOutputSpec is one declared output. Mode defaults to "exclusive", the only
// mode the schema admits today.
type TaskOutputSpec struct {
	Path string
	Mode string
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

	if err := s.writeGraphTasksOn(ctx, tx, planID, o.Tasks); err != nil {
		return "", err
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

// MaterializePlanGraph writes tasks, metadata, and edges for a plan that
// already exists, in one transaction.
//
// This is the graph half of CreatePlanGraph without the plan row. Dispatch uses
// it for a plan that reached the store outside the import path — one written by
// CreatePlan directly, or any plan predating the graph — which has source
// markdown but no nodes. Without it the edge walk would report such a plan as
// having nothing ready and it would silently never dispatch.
//
// The caller decides WHEN this is appropriate (dispatch only calls it for a
// plan with no tasks at all). This is deliberately not an upsert: re-deriving
// edges under a plan mid-run could contradict decisions the run has already
// made, so a duplicate task id fails the transaction rather than merging.
func (s *Store) MaterializePlanGraph(ctx context.Context, planID string, tasks []GraphTaskSpec) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin materialize plan graph: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.writeGraphTasksOn(ctx, tx, planID, tasks); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit materialize plan graph: %w", err)
	}
	return nil
}

// writeGraphTasksOn writes every node, its metadata, and then every edge, on
// the caller's transaction. Shared by CreatePlanGraph and MaterializePlanGraph
// so there is one implementation of the node-then-edge ordering.
func (s *Store) writeGraphTasksOn(ctx context.Context, ex execer, planID string, tasks []GraphTaskSpec) error {
	for _, task := range tasks {
		spec := task.CreateTaskOpts
		spec.PlanID = planID
		if err := s.createTaskOn(ctx, ex, spec); err != nil {
			return err
		}
		if task.GroupPath != "" {
			metadata := task.MetadataJSON
			if metadata == "" {
				metadata = "{}"
			}
			if err := s.putTaskMetadataOn(
				ctx, ex, planID, spec.ID, task.GroupPath, task.TeamPath, metadata,
			); err != nil {
				return err
			}
		}
		// Reservations go in the SAME transaction as the node. A task that
		// exists without its reservations would be claimable against a path a
		// peer owns, which is the window reservations exist to close.
		for _, in := range task.Inputs {
			if err := s.reserveTaskInputOn(ctx, ex, planID, spec.ID, in.Path, in.SHA256); err != nil {
				return err
			}
		}
		for _, out := range task.Outputs {
			if err := s.reserveTaskOutputOn(ctx, ex, planID, spec.ID, out.Path, out.Mode); err != nil {
				return err
			}
		}
	}

	// Edges go last so every referenced task already exists — the FK on
	// task_deps would otherwise reject a forward reference, and a plan is
	// perfectly entitled to declare `after` against a step written below it.
	for _, task := range tasks {
		for _, dep := range task.DependsOn {
			if err := s.addDepOn(ctx, ex, planID, task.ID, dep); err != nil {
				return err
			}
		}
	}
	return nil
}
