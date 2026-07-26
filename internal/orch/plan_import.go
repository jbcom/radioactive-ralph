package orch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// ErrInvalidPlanContract classifies a syntactically valid markdown plan whose
// v2 provider/capability contract cannot be admitted.
var ErrInvalidPlanContract = errors.New("orch: invalid plan contract")

// ImportPlanOpts is the shared CLI/supervisor plan ingress contract.
type ImportPlanOpts struct {
	ProjectID string
	Slug      string
	Title     string
	Markdown  string
}

// ImportPlan validates and activates a legacy plan, or atomically materializes
// and activates every task and edge of a ralph.plan/v2 graph.
func (o *Orchestrator) ImportPlan(ctx context.Context, opts ImportPlanOpts) (string, error) {
	if err := plan.ValidateForImport([]byte(opts.Markdown)); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPlanContract, err)
	}
	parsed, err := plan.Parse([]byte(opts.Markdown))
	if err != nil {
		return "", fmt.Errorf("orch: parse plan: %w", err)
	}
	if !parsed.V2 {
		return o.importLegacyPlan(ctx, opts)
	}
	projectDir, found, err := o.store.ProjectAbsPath(ctx, opts.ProjectID)
	if err != nil {
		return "", fmt.Errorf("orch: resolve v2 project path: %w", err)
	}
	if !found {
		return "", fmt.Errorf("%w: project %s has no absolute checkout path", ErrInvalidPlanContract, opts.ProjectID)
	}
	bindings, err := o.validateV2Bindings(ctx, opts.ProjectID, parsed)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPlanContract, err)
	}

	tasks := make([]store.GraphTaskSpec, 0, len(parsed.V2Tasks()))
	for _, task := range parsed.V2Tasks() {
		metadata := task.Step.Metadata
		if err := validateV2Filesystem(projectDir, metadata); err != nil {
			return "", fmt.Errorf("%w: task %s filesystem admission: %v", ErrInvalidPlanContract, metadata.ID, err)
		}
		raw, err := json.Marshal(metadata)
		if err != nil {
			return "", fmt.Errorf("orch: marshal task %s metadata: %w", metadata.ID, err)
		}
		acceptance, err := strictV2AcceptanceJSON(task.Step, projectDir)
		if err != nil {
			return "", fmt.Errorf("%w: task %s acceptance: %v", ErrInvalidPlanContract, metadata.ID, err)
		}
		outputs := make([]store.OutputReservation, 0, len(metadata.Outputs))
		for _, output := range metadata.Outputs {
			outputs = append(outputs, store.OutputReservation{Path: output.Path, Mode: output.Mode})
		}
		inputs := make([]store.InputReservation, 0, len(metadata.Inputs))
		for _, input := range metadata.Inputs {
			inputs = append(inputs, store.InputReservation{Path: input.Path, SHA256: input.SHA256})
		}
		tasks = append(tasks, store.GraphTaskSpec{
			ID: metadata.ID, Description: task.Step.Text, TeamPath: metadata.Team,
			MetadataJSON: string(raw), AcceptanceJSON: acceptance,
			CalibrationID:     bindings[metadata.ID].calibrationID,
			CapabilitySetJSON: bindings[metadata.ID].capabilitySetJSON,
			DependsOn:         append([]string{}, metadata.After...), Inputs: inputs, Outputs: outputs,
			RequiresApproval: task.Step.RequiresApproval, Order: task.Order,
		})
	}
	return o.store.CreatePlanGraph(ctx, store.CreatePlanGraphOpts{
		Plan: store.CreatePlanOpts{
			ProjectID: opts.ProjectID, Slug: opts.Slug, Title: opts.Title,
			SourceMarkdown: opts.Markdown,
		},
		Status: store.PlanStatusActive,
		Tasks:  tasks,
	})
}

func (o *Orchestrator) importLegacyPlan(ctx context.Context, opts ImportPlanOpts) (string, error) {
	planID, err := o.store.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID: opts.ProjectID, Slug: opts.Slug, Title: opts.Title,
		SourceMarkdown: opts.Markdown,
	})
	if err != nil {
		return "", err
	}
	if err := o.store.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		return "", fmt.Errorf("orch: activate legacy plan: %w", err)
	}
	return planID, nil
}

type resolvedTaskBinding struct {
	calibrationID     string
	capabilitySetJSON string
}

func (o *Orchestrator) validateV2Bindings(
	ctx context.Context,
	projectID string,
	parsed *plan.Plan,
) (map[string]resolvedTaskBinding, error) {
	resolved := make(map[string]resolvedTaskBinding, len(parsed.V2Tasks()))
	for _, task := range parsed.V2Tasks() {
		metadata := task.Step.Metadata
		taskBinding := resolvedTaskBinding{}
		switch metadata.Binding.Mode {
		case "pool":
			for _, requirement := range metadata.Requires {
				if !provider.KnownCapability(requirement) {
					return nil, fmt.Errorf(
						"orch: task %s requires unknown capability %q",
						metadata.ID, requirement,
					)
				}
				if provider.CalibrationRequiredCapability(requirement) {
					return nil, fmt.Errorf(
						"orch: task %s requires measured capability %q but uses an uncalibrated pool",
						metadata.ID, requirement,
					)
				}
			}
			for _, name := range metadata.Providers {
				if _, err := provider.ResolveShippedBinding(name); err != nil {
					return nil, fmt.Errorf("orch: task %s provider %q: %w", metadata.ID, name, err)
				}
			}
			if _, err := o.resolveConstrainedBinding(
				ctx, projectID, false, BindingProbe,
				BindingConstraints{
					AllowedProviders: append([]string{}, metadata.Providers...),
					Requirements:     append([]string{}, metadata.Requires...),
				},
			); err != nil {
				return nil, fmt.Errorf("orch: task %s provider admission: %w", metadata.ID, err)
			}
		case "calibrated":
			calibration, err := o.store.GetProviderCalibration(ctx, metadata.Binding.Calibration)
			if err != nil {
				return nil, fmt.Errorf("orch: task %s calibration: %w", metadata.ID, err)
			}
			taskBinding, err = validateTaskCalibration(metadata, calibration)
			if err != nil {
				return nil, fmt.Errorf("orch: task %s calibrated binding: %w", metadata.ID, err)
			}
		case "await-calibration":
			if err := validateExactTaskBinding(metadata); err != nil {
				return nil, fmt.Errorf("orch: task %s awaiting binding: %w", metadata.ID, err)
			}
			calibration, err := o.store.GetProviderCalibrationByAlias(ctx, metadata.Binding.Alias)
			if err == nil {
				taskBinding, err = validateTaskCalibration(metadata, calibration)
				if err != nil {
					return nil, fmt.Errorf("orch: task %s awaiting calibration: %w", metadata.ID, err)
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("orch: task %s lookup awaiting calibration: %w", metadata.ID, err)
			}
		case "calibration":
			if err := validateExactTaskBinding(metadata); err != nil {
				return nil, fmt.Errorf("orch: task %s calibration fixture binding: %w", metadata.ID, err)
			}
		default:
			return nil, fmt.Errorf("orch: task %s unsupported binding mode %q", metadata.ID, metadata.Binding.Mode)
		}
		resolved[metadata.ID] = taskBinding
	}
	return resolved, nil
}

func validateExactTaskBinding(metadata *plan.TaskMetadata) error {
	binding, err := provider.ResolveShippedBinding(metadata.Binding.Provider)
	if err != nil {
		return fmt.Errorf("resolve exact provider: %w", err)
	}
	binding.Name = metadata.Binding.Alias
	if _, err := provider.ResolveInvocation(binding, provider.Request{
		Model: provider.Model(metadata.Binding.Model), Effort: metadata.Binding.Effort,
		StrictBinding: true,
	}); err != nil {
		return fmt.Errorf("resolve exact invocation: %w", err)
	}
	if len(metadata.Providers) > 0 &&
		!slices.Contains(metadata.Providers, metadata.Binding.Provider) &&
		!slices.Contains(metadata.Providers, metadata.Binding.Alias) {
		return fmt.Errorf(
			"exact provider %s alias %s is outside its allowlist",
			metadata.Binding.Provider, metadata.Binding.Alias,
		)
	}
	for _, name := range metadata.Providers {
		if name == metadata.Binding.Provider || name == metadata.Binding.Alias {
			continue
		}
		if _, err := provider.ResolveShippedBinding(name); err != nil {
			return fmt.Errorf("provider allowlist entry %q: %w", name, err)
		}
	}
	for _, requirement := range metadata.Requires {
		if !provider.KnownCapability(requirement) {
			return fmt.Errorf("unknown capability %q", requirement)
		}
	}
	return nil
}

func validateTaskCalibration(
	metadata *plan.TaskMetadata,
	calibration store.ProviderCalibration,
) (resolvedTaskBinding, error) {
	if err := validateExactTaskBinding(metadata); err != nil {
		return resolvedTaskBinding{}, err
	}
	if metadata.Binding.Alias != calibration.Alias ||
		metadata.Binding.Provider != calibration.Provider ||
		metadata.Binding.Model != calibration.Model ||
		metadata.Binding.Effort != calibration.Effort {
		return resolvedTaskBinding{}, fmt.Errorf(
			"binding tuple does not exactly match calibration %s",
			calibration.ID,
		)
	}
	binding, err := ValidateProviderCalibration(calibration)
	if err != nil {
		return resolvedTaskBinding{}, err
	}
	if !binding.SupportsRequirements(metadata.Requires) {
		return resolvedTaskBinding{}, fmt.Errorf(
			"calibration %s lacks one or more required capabilities %v",
			calibration.ID, metadata.Requires,
		)
	}
	capabilityJSON, err := json.Marshal(calibration.Capabilities)
	if err != nil {
		return resolvedTaskBinding{}, fmt.Errorf("marshal calibration capabilities: %w", err)
	}
	return resolvedTaskBinding{
		calibrationID: calibration.ID, capabilitySetJSON: string(capabilityJSON),
	}, nil
}

func calibratedProviderBinding(calibration store.ProviderCalibration) (provider.Binding, error) {
	binding, err := provider.ResolveShippedBinding(calibration.Provider)
	if err != nil {
		return provider.Binding{}, err
	}
	binding.Name = calibration.Alias
	binding.CalibratedCapabilities = append([]string{}, calibration.Capabilities...)
	return binding, nil
}
