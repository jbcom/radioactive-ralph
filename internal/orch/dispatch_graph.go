package orch

import (
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// readyWave is what dispatch acts on: the store's answer to "what may run right
// now", already partitioned into leaf groups, with each ready task resolved
// back to the plan step that describes it.
type readyWave struct {
	partitions []readyGroup
}

// readyGroup is one leaf group's worth of simultaneously-ready work. It is the
// unit native fan-out may delegate to a single provider turn: one group, one
// heading, one resolved binding.
type readyGroup struct {
	groupPath string
	heading   string
	// parallel reports whether the steps in this group are mutually
	// independent — true when the group holds more than one simultaneously
	// ready task, which by construction of the edge walk means none of them
	// depends on another.
	parallel bool
	refs     []plan.StepRef
	steps    []plan.Step
	tasks    []*store.Task
}

// loadReadyWave asks the store which tasks are ready (the task_deps walk) and
// resolves each back to its plan step.
//
// This is the increment: readiness comes from the GRAPH, not from re-parsing
// the markdown and recomputing positions. plan.Decompose could only ever
// surface one leaf group per pass, because it walked groups in document order
// and stopped at the first with unfinished work — so a plan that fans into two
// independent branches serialized them for no reason. The edge walk has no such
// limitation, and group_path preserves the grouping that fan-out needs.
func (o *Orchestrator) loadReadyWave(ctx context.Context, planID string, parsed *plan.Plan) (readyWave, error) {
	partitions, err := o.store.ReadyPartitions(ctx, planID)
	if err != nil {
		return readyWave{}, fmt.Errorf("orch: ready partitions: %w", err)
	}
	index := indexPlanSteps(parsed)

	wave := readyWave{}
	for _, part := range partitions {
		group := readyGroup{
			groupPath: part.GroupPath,
			parallel:  len(part.Tasks) > 1,
		}
		for i := range part.Tasks {
			task := part.Tasks[i]
			located, ok := index[task.ID]
			if !ok {
				// A ready task with no matching step in the plan markdown means
				// the stored graph and the stored source have diverged. Refuse
				// rather than dispatch a worker with no instructions.
				return readyWave{}, fmt.Errorf(
					"orch: ready task %q has no step in the plan source", task.ID)
			}
			group.refs = append(group.refs, located.ref)
			group.steps = append(group.steps, located.step)
			group.tasks = append(group.tasks, &task)
		}
		if len(group.refs) > 0 {
			group.heading = groupHeadingFor(parsed, group.refs[0])
		}
		wave.partitions = append(wave.partitions, group)
	}
	return wave, nil
}

// locatedStep pairs a plan step with the positional ref it was found at.
type locatedStep struct {
	ref  plan.StepRef
	step plan.Step
}

// stepTaskID is THE rule mapping a plan step to its store task id: an explicit
// `id` from the step's ralph-task metadata when present, else the positional
// StepRef id every plan used before that grammar existed.
//
// Import and dispatch must agree on this exactly. If they disagreed, dispatch
// would materialize a second task row for a step the import had already
// created, and the plan would run each annotated step twice — so the rule lives
// in one function that both call.
func stepTaskID(ref plan.StepRef, step plan.Step) string {
	if step.Metadata != nil && step.Metadata.ID != "" {
		return step.Metadata.ID
	}
	return ref.ID()
}

// indexPlanSteps maps every step in the plan to the task id it was imported
// under, so dispatch can resolve a stored task id back to its instructions
// without knowing whether the plan was annotated.
func indexPlanSteps(parsed *plan.Plan) map[string]locatedStep {
	index := map[string]locatedStep{}
	walk(parsed.Groups, nil, func(ref plan.StepRef, step plan.Step, _ string) {
		index[stepTaskID(ref, step)] = locatedStep{ref: ref, step: step}
	})
	return index
}
