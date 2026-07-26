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
		constraints := BindingConstraints{
			AllowedProviders: append([]string{}, metadata.Providers...),
			DeniedProviders:  denied,
			Requirements:     append([]string{}, metadata.Requires...),
		}
		var model provider.Model
		var effort string
		var bindingOverride *provider.Binding
		var independenceDomain string
		calibrationMode := ""
		calibrationRepetitions := 0
		calibrationFixture := ""
		switch metadata.Binding.Mode {
		case "calibrated", "await-calibration":
			var calibration store.ProviderCalibration
			if metadata.Binding.Mode == "calibrated" {
				calibration, err = o.store.GetProviderCalibration(ctx, metadata.Binding.Calibration)
			} else {
				calibration, err = o.store.GetProviderCalibrationByAlias(ctx, metadata.Binding.Alias)
			}
			if err != nil {
				if metadata.Binding.Mode == "await-calibration" && errors.Is(err, sql.ErrNoRows) {
					if blockErr := o.store.MarkBlockedCapability(
						ctx, planID, metadata.ID,
						fmt.Sprintf("awaiting immutable calibration for alias %s", metadata.Binding.Alias),
					); blockErr != nil {
						return dispatched, fmt.Errorf("orch: persist calibration wait: %w", blockErr)
					}
					continue
				}
				return dispatched, fmt.Errorf("orch: load task calibration: %w", err)
			}
			resolvedCalibration, validationErr := validateTaskCalibration(metadata, calibration)
			if validationErr != nil {
				if blockErr := o.store.MarkBlockedCapability(
					ctx, planID, metadata.ID, validationErr.Error(),
				); blockErr != nil {
					return dispatched, fmt.Errorf("orch: persist calibration validation block: %w", blockErr)
				}
				continue
			}
			if slices.Contains(denied, calibration.IndependenceDomain) {
				if blockErr := o.store.MarkBlockedCapability(
					ctx, planID, metadata.ID,
					fmt.Sprintf("independence domain %s already used by separated dependency", calibration.IndependenceDomain),
				); blockErr != nil {
					return dispatched, fmt.Errorf("orch: persist independence block: %w", blockErr)
				}
				continue
			}
			binding, err := ValidateProviderCalibration(calibration)
			if err != nil {
				if blockErr := o.store.MarkBlockedCapability(
					ctx, planID, metadata.ID, err.Error(),
				); blockErr != nil {
					return dispatched, fmt.Errorf("orch: persist calibrated provider block: %w", blockErr)
				}
				continue
			}
			if metadata.Binding.Mode == "await-calibration" {
				if err := o.store.BindTaskCalibration(
					ctx, planID, metadata.ID,
					resolvedCalibration.calibrationID, resolvedCalibration.capabilitySetJSON,
				); err != nil {
					if blockErr := o.store.MarkBlockedCapability(
						ctx, planID, metadata.ID, err.Error(),
					); blockErr != nil {
						return dispatched, fmt.Errorf("orch: persist calibration snapshot block: %w", blockErr)
					}
					continue
				}
			}
			bindingOverride = &binding
			constraints = BindingConstraints{}
			model, effort = provider.Model(calibration.Model), calibration.Effort
			independenceDomain = calibration.IndependenceDomain
		case "calibration":
			binding, err := provider.ResolveShippedBinding(metadata.Binding.Provider)
			if err != nil {
				return dispatched, fmt.Errorf("orch: resolve calibration provider: %w", err)
			}
			binding.Name = metadata.Binding.Alias
			if _, err := provider.ResolveInvocation(binding, provider.Request{
				Model:  provider.Model(metadata.Binding.Model),
				Effort: metadata.Binding.Effort, StrictBinding: true,
			}); err != nil {
				return dispatched, fmt.Errorf("orch: resolve calibration invocation: %w", err)
			}
			bindingOverride = &binding
			constraints = BindingConstraints{}
			model, effort = provider.Model(metadata.Binding.Model), metadata.Binding.Effort
			independenceDomain = binding.Config.Type
			calibrationMode = metadata.Binding.Mode
			calibrationRepetitions = metadata.Binding.Repetitions
			calibrationFixture = metadata.Binding.Fixture
		}
		if !o.acquireDispatchSlot() {
			break
		}
		launched, err := o.dispatchReadyStep(ctx, dispatchStepArgs{
			projectID: projectID, projectDir: projectDir, planID: planID,
			parsedPlan: parsed, storeTitle: storedPlan.Title,
			groupHeading: v2Task.GroupHeading, step: v2Task.Step,
			constraints: &constraints, bindingOverride: bindingOverride,
			model: model, effort: effort, independenceDomain: independenceDomain,
			calibrationMode: calibrationMode, calibrationRepetitions: calibrationRepetitions,
			calibrationFixture: calibrationFixture,
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
