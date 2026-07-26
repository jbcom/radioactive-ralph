package orch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/provider"
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

	dispatched := 0
	for i := range ready {
		if o.maxParallel > 0 && dispatched >= o.maxParallel {
			break
		}
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
		denied, err := o.separatedDomains(ctx, planID, metadata)
		if err != nil {
			if errors.Is(err, ErrNoCapableProvider) {
				if blockErr := o.store.MarkBlockedCapability(
					ctx, planID, metadata.ID, err.Error(),
				); blockErr != nil {
					return dispatched, fmt.Errorf("orch: persist separation block: %w", blockErr)
				}
				continue
			}
			return dispatched, err
		}
		dispatchBinding, blockReason, err := o.resolveV2DispatchBinding(
			ctx, planID, metadata, denied,
		)
		if err != nil {
			return dispatched, err
		}
		if blockReason != "" {
			if blockErr := o.store.MarkBlockedCapability(
				ctx, planID, metadata.ID, blockReason,
			); blockErr != nil {
				return dispatched, fmt.Errorf("orch: persist capability block: %w", blockErr)
			}
			continue
		}
		if !o.acquireDispatchSlot() {
			break
		}
		launched, err := o.dispatchReadyStep(ctx, dispatchStepArgs{
			projectID: projectID, projectDir: projectDir, planID: planID,
			parsedPlan: parsed, storeTitle: storedPlan.Title,
			groupHeading: v2Task.GroupHeading, step: v2Task.Step,
			constraints:     &dispatchBinding.constraints,
			bindingOverride: dispatchBinding.bindingOverride,
			model:           dispatchBinding.model, effort: dispatchBinding.effort,
			independenceDomain:     dispatchBinding.independenceDomain,
			calibrationMode:        dispatchBinding.calibrationMode,
			calibrationRepetitions: dispatchBinding.calibrationRepetitions,
			calibrationFixture:     dispatchBinding.calibrationFixture,
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

type v2DispatchBinding struct {
	constraints            BindingConstraints
	bindingOverride        *provider.Binding
	model                  provider.Model
	effort                 string
	independenceDomain     string
	calibrationMode        string
	calibrationRepetitions int
	calibrationFixture     string
}

func (o *Orchestrator) resolveV2DispatchBinding(
	ctx context.Context,
	planID string,
	metadata *plan.TaskMetadata,
	denied []string,
) (v2DispatchBinding, string, error) {
	resolved := v2DispatchBinding{constraints: BindingConstraints{
		AllowedProviders: append([]string{}, metadata.Providers...),
		DeniedProviders:  denied,
		Requirements:     append([]string{}, metadata.Requires...),
	}}
	switch metadata.Binding.Mode {
	case "pool":
		return resolved, "", nil
	case "calibration":
		binding, err := provider.ResolveShippedBinding(metadata.Binding.Provider)
		if err != nil {
			return v2DispatchBinding{}, "", fmt.Errorf("orch: resolve calibration provider: %w", err)
		}
		binding.Name = metadata.Binding.Alias
		if _, err := provider.ResolveInvocation(binding, provider.Request{
			Model: provider.Model(metadata.Binding.Model), Effort: metadata.Binding.Effort,
			StrictBinding: true,
		}); err != nil {
			return v2DispatchBinding{}, "", fmt.Errorf("orch: resolve calibration invocation: %w", err)
		}
		resolved.constraints = BindingConstraints{}
		resolved.bindingOverride = &binding
		resolved.model = provider.Model(metadata.Binding.Model)
		resolved.effort = metadata.Binding.Effort
		resolved.independenceDomain = binding.Config.Type
		resolved.calibrationMode = metadata.Binding.Mode
		resolved.calibrationRepetitions = metadata.Binding.Repetitions
		resolved.calibrationFixture = metadata.Binding.Fixture
		return resolved, "", nil
	}

	calibration, err := o.loadTaskCalibration(ctx, metadata)
	if err != nil {
		if metadata.Binding.Mode == "await-calibration" && errors.Is(err, sql.ErrNoRows) {
			return v2DispatchBinding{}, fmt.Sprintf(
				"awaiting immutable calibration for alias %s", metadata.Binding.Alias,
			), nil
		}
		return v2DispatchBinding{}, "", fmt.Errorf("orch: load task calibration: %w", err)
	}
	snapshot, err := validateTaskCalibration(metadata, calibration)
	if err != nil {
		return v2DispatchBinding{}, err.Error(), nil
	}
	if slices.Contains(denied, calibration.IndependenceDomain) {
		return v2DispatchBinding{}, fmt.Sprintf(
			"independence domain %s already used by separated dependency",
			calibration.IndependenceDomain,
		), nil
	}
	binding, err := ValidateProviderCalibration(calibration)
	if err != nil {
		return v2DispatchBinding{}, err.Error(), nil
	}
	if metadata.Binding.Mode == "await-calibration" {
		if err := o.store.BindTaskCalibration(
			ctx, planID, metadata.ID, snapshot.calibrationID, snapshot.capabilitySetJSON,
		); err != nil {
			return v2DispatchBinding{}, err.Error(), nil
		}
	}
	resolved.constraints = BindingConstraints{}
	resolved.bindingOverride = &binding
	resolved.model = provider.Model(calibration.Model)
	resolved.effort = calibration.Effort
	resolved.independenceDomain = calibration.IndependenceDomain
	return resolved, "", nil
}

func (o *Orchestrator) loadTaskCalibration(
	ctx context.Context,
	metadata *plan.TaskMetadata,
) (store.ProviderCalibration, error) {
	if metadata.Binding.Mode == "calibrated" {
		return o.store.GetProviderCalibration(ctx, metadata.Binding.Calibration)
	}
	return o.store.GetProviderCalibrationByAlias(ctx, metadata.Binding.Alias)
}

func (o *Orchestrator) separatedDomains(
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
		if execution.AssignedIndependenceDomain == "" {
			return nil, noCapableProvider("dependency %s has no independence-domain provenance", taskID)
		}
		if !seen[execution.AssignedIndependenceDomain] {
			seen[execution.AssignedIndependenceDomain] = true
			denied = append(denied, execution.AssignedIndependenceDomain)
		}
	}
	return denied, nil
}
