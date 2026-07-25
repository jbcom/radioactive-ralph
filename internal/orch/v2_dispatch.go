package orch

import (
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

func (o *Orchestrator) dispatchNextV2(
	ctx context.Context,
	projectID, planID string,
	storedPlan *store.Plan,
	parsed *plan.Plan,
) (int, error) {
	ready, err := o.store.Ready(ctx, planID)
	if err != nil {
		return 0, fmt.Errorf("orch: list v2 ready tasks: %w", err)
	}
	if len(ready) == 0 {
		return 0, nil
	}
	byID := make(map[string]plan.V2Task, len(parsed.V2Tasks()))
	for _, task := range parsed.V2Tasks() {
		byID[task.Step.Metadata.ID] = task
	}
	projectDir, err := o.projectDirFor(ctx, planID)
	if err != nil {
		return 0, err
	}

	limit := len(ready)
	if o.maxParallel > 0 && limit > o.maxParallel {
		limit = o.maxParallel
	}
	dispatched := 0
	for i := 0; i < limit; i++ {
		v2Task, ok := byID[ready[i].ID]
		if !ok {
			return dispatched, fmt.Errorf("orch: materialized v2 task %s missing from source", ready[i].ID)
		}
		metadata := v2Task.Step.Metadata
		if err := validateV2Filesystem(projectDir, metadata); err != nil {
			if blockErr := o.store.MarkBlockedInput(ctx, planID, metadata.ID, err.Error()); blockErr != nil {
				return dispatched, fmt.Errorf("orch: persist input block: %w", blockErr)
			}
			continue
		}
		denied, err := o.separatedProviders(ctx, planID, metadata)
		if err != nil {
			return dispatched, err
		}
		constraints := BindingConstraints{
			AllowedProviders: append([]string{}, metadata.Providers...),
			DeniedProviders:  denied,
			Requirements:     append([]string{}, metadata.Requires...),
		}
		if !o.acquireDispatchSlot() {
			break
		}
		launched, err := o.dispatchReadyStep(ctx, dispatchStepArgs{
			projectID: projectID, projectDir: projectDir, planID: planID,
			parsedPlan: parsed, storeTitle: storedPlan.Title,
			groupHeading: v2Task.GroupHeading, step: v2Task.Step,
			constraints: &constraints,
		})
		if err != nil {
			return dispatched, err
		}
		if launched {
			dispatched++
		}
	}
	return dispatched, nil
}

func (o *Orchestrator) separatedProviders(
	ctx context.Context,
	planID string,
	metadata *plan.TaskMetadata,
) ([]string, error) {
	seen := map[string]bool{}
	var denied []string
	for _, taskID := range metadata.DifferentFrom {
		execution, err := o.store.GetTaskExecutionMetadata(ctx, planID, taskID)
		if err != nil {
			return nil, fmt.Errorf("orch: load provider provenance for %s: %w", taskID, err)
		}
		if execution.AssignedProvider == "" {
			return nil, fmt.Errorf("orch: dependency %s has no provider provenance", taskID)
		}
		if !seen[execution.AssignedProvider] {
			seen[execution.AssignedProvider] = true
			denied = append(denied, execution.AssignedProvider)
		}
	}
	return denied, nil
}
