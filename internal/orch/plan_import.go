package orch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	if err := o.validateV2Bindings(ctx, opts.ProjectID, parsed); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPlanContract, err)
	}

	tasks := make([]store.GraphTaskSpec, 0, len(parsed.V2Tasks()))
	for _, task := range parsed.V2Tasks() {
		metadata := task.Step.Metadata
		raw, err := json.Marshal(metadata)
		if err != nil {
			return "", fmt.Errorf("orch: marshal task %s metadata: %w", metadata.ID, err)
		}
		acceptance, err := defaultAcceptanceJSON(task.Step)
		if err != nil {
			return "", err
		}
		outputs := make([]store.OutputReservation, 0, len(metadata.Outputs))
		for _, output := range metadata.Outputs {
			outputs = append(outputs, store.OutputReservation{Path: output.Path, Mode: output.Mode})
		}
		tasks = append(tasks, store.GraphTaskSpec{
			ID: metadata.ID, Description: task.Step.Text, TeamPath: metadata.Team,
			MetadataJSON: string(raw), AcceptanceJSON: acceptance,
			DependsOn: append([]string{}, metadata.After...), Outputs: outputs,
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

func (o *Orchestrator) validateV2Bindings(ctx context.Context, projectID string, parsed *plan.Plan) error {
	for _, task := range parsed.V2Tasks() {
		metadata := task.Step.Metadata
		for _, requirement := range metadata.Requires {
			if !provider.KnownCapability(requirement) {
				return fmt.Errorf("orch: task %s requires unknown capability %q", metadata.ID, requirement)
			}
		}
		for _, name := range metadata.Providers {
			if _, err := provider.ResolveShippedBinding(name); err != nil {
				return fmt.Errorf("orch: task %s provider %q: %w", metadata.ID, name, err)
			}
		}
		_, err := o.resolveConstrainedBinding(
			ctx, projectID, false, BindingProbe,
			BindingConstraints{
				AllowedProviders: metadata.Providers,
				Requirements:     metadata.Requires,
			},
		)
		if err != nil {
			return fmt.Errorf("orch: task %s provider admission: %w", metadata.ID, err)
		}
	}
	return nil
}
