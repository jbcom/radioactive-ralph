package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/fixit"
	"github.com/jbcom/radioactive-ralph/internal/plandag"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// runAdvisor is the fixit-only code path. It drives the fixit
// six-stage pipeline (see jonbogaty.com/radioactive-ralph/design/)
// and emits both a repo-visible advisor report and a durable DAG plan.
// When a plan with the same slug already exists for this repo, the
// markdown report is refreshed and the DAG is left untouched.
func (c *RunCmd) runAdvisor(ctx context.Context, repo string, plansOK bool) error {
	// Pull defaults from config.toml [variants.fixit] when CLI flags
	// aren't set. Precedence: CLI > config.toml > built-in defaults.
	var fromConfig config.VariantFile
	if cfg, err := config.Load(repo); err == nil && cfg.Variants != nil {
		fromConfig = cfg.Variants["fixit"]
	}

	maxIter := c.MaxIterations
	if maxIter == 0 && fromConfig.MaxRefinementIterations != nil {
		maxIter = *fromConfig.MaxRefinementIterations
	}
	minConf := c.MinConfidence
	if minConf == 0 && fromConfig.MinConfidenceThreshold != nil {
		minConf = *fromConfig.MinConfidenceThreshold
	}
	planModel := c.PlanModel
	if planModel == "" {
		planModel = fromConfig.PlanModel
	}
	planEffort := c.PlanEffort
	if planEffort == "" {
		planEffort = fromConfig.PlanEffort
	}
	binding, err := resolveFixitPlanningBinding(repo, fromConfig)
	if err != nil {
		return err
	}

	opts := fixit.RunOptions{
		RepoRoot:                repo,
		Topic:                   c.Topic,
		Description:             c.Description,
		NonInteractive:          !interactiveTerminal(),
		ProviderBinding:         binding,
		MaxRefinementIterations: maxIter,
		MinConfidenceThreshold:  minConf,
		PlanModel:               planModel,
		PlanEffort:              planEffort,
	}

	result, err := fixit.RunPipeline(ctx, opts)
	if err != nil {
		return fmt.Errorf("fixit pipeline: %w", err)
	}

	fmt.Printf("radioactive_ralph: fixit advisor wrote %s (status=%s, confidence=%d)\n",
		result.Path, result.Status, result.Proposal.Confidence)
	if result.Proposal.Primary != "" {
		fmt.Printf("  primary recommendation: %s-ralph\n", result.Proposal.Primary)
	}
	if result.Proposal.Alternate != "" {
		fmt.Printf("  alternate: %s-ralph — %s\n",
			result.Proposal.Alternate, result.Proposal.AlternateWhen)
	}
	if !result.Validation.Passed {
		fmt.Printf("  validation: %d rule(s) failed (status=%s)\n",
			len(result.Validation.Failures), result.Status)
	}
	if result.Status != fixit.StatusFallback {
		dagResult, err := syncAdvisorPlanToDAG(ctx, repo, result)
		if err != nil {
			return err
		}
		if dagResult.planID != "" {
			fmt.Printf("  durable plan: %s (status=%s)\n", dagResult.planID, dagResult.status)
		} else {
			fmt.Printf("  durable plan: unchanged (%s)\n", dagResult.note)
		}
	}

	// Auto-handoff: only when status=current AND no alternate
	// (unambiguous).
	if c.AutoHandoff {
		switch {
		case result.Status != fixit.StatusCurrent:
			fmt.Println("radioactive_ralph: --auto-handoff skipped — plan status is not `current`")
		case result.Proposal.Alternate != "":
			fmt.Println("radioactive_ralph: --auto-handoff skipped — recommendation has tradeoffs")
		default:
			fmt.Printf("radioactive_ralph: --auto-handoff → starting %s-ralph\n",
				result.Proposal.Primary)
			return (&RunCmd{
				Variant:  result.Proposal.Primary,
				RepoRoot: repo,
			}).Run(&runContext{ctx: ctx})
		}
	}

	if result.Status == fixit.StatusFallback {
		return fmt.Errorf("fixit emitted a fallback plan — operator intervention required")
	}

	if !plansOK {
		fmt.Println("radioactive_ralph: no active plan existed for this repo; fixit ran as the plans-first fallback.")
	}
	return nil
}

type dagSyncResult struct {
	planID string
	status plandag.PlanStatus
	note   string
}

func syncAdvisorPlanToDAG(ctx context.Context, repo string, result fixit.EmittedPlan) (dagSyncResult, error) {
	store, err := openPlanStore(ctx)
	if err != nil {
		return dagSyncResult{}, fmt.Errorf("open plan store: %w", err)
	}
	defer func() { _ = store.Close() }()

	topic := strings.TrimSuffix(filepath.Base(result.Path), "-advisor.md")
	plans, err := store.ListPlans(ctx, []plandag.PlanStatus{
		plandag.PlanStatusDraft,
		plandag.PlanStatusActive,
		plandag.PlanStatusPaused,
		plandag.PlanStatusDone,
		plandag.PlanStatusFailedPartial,
		plandag.PlanStatusArchived,
		plandag.PlanStatusAbandoned,
	})
	if err != nil {
		return dagSyncResult{}, fmt.Errorf("list plans: %w", err)
	}
	for _, plan := range plans {
		if plan.RepoPath == repo && plan.Slug == topic {
			return dagSyncResult{note: "slug already exists for this repo"}, nil
		}
	}

	emitted, err := fixit.EmitToDAG(ctx, fixit.EmitToDAGOpts{
		Store:      store,
		Topic:      topic,
		Proposal:   result.Proposal,
		Validation: result.Validation,
		Status:     result.Status,
		Intent:     result.Intent,
		RC:         result.RepoContext,
	})
	if err != nil {
		return dagSyncResult{}, fmt.Errorf("sync advisor DAG: %w", err)
	}
	return dagSyncResult{
		planID: emitted.PlanID,
		status: mapFixitStatusToPlanStatus(result.Status),
	}, nil
}

func mapFixitStatusToPlanStatus(status fixit.PlanStatus) plandag.PlanStatus {
	switch status {
	case fixit.StatusCurrent:
		return plandag.PlanStatusActive
	case fixit.StatusProvisional:
		return plandag.PlanStatusDraft
	default:
		return plandag.PlanStatusDraft
	}
}

// interactiveTerminal reports whether stdin is a terminal (TTY). Used
// to pick between Stage 1's interactive Q&A and the non-interactive
// pass-through.
func interactiveTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func resolveFixitPlanningBinding(repo string, fromConfig config.VariantFile) (provider.Binding, error) {
	cfg, err := config.Load(repo)
	if err != nil && !config.IsMissingConfig(err) {
		return provider.Binding{}, fmt.Errorf("config.Load: %w", err)
	}
	local, err := config.LoadLocal(repo)
	if err != nil && !config.IsMissingLocal(err) {
		return provider.Binding{}, fmt.Errorf("config.LoadLocal: %w", err)
	}
	fixitProfile, err := variant.Lookup("fixit")
	if err != nil {
		return provider.Binding{}, err
	}
	binding, err := provider.ResolveBinding(cfg, local, fixitProfile, fromConfig)
	if err != nil {
		return provider.Binding{}, err
	}
	if binding.Config.Binary == "" {
		return provider.Binding{}, fmt.Errorf("provider %q has no configured binary", binding.Name)
	}
	if err := provider.ValidateBinding(binding); err != nil {
		return provider.Binding{}, err
	}
	if _, err := exec.LookPath(binding.Config.Binary); err != nil {
		return provider.Binding{}, fmt.Errorf("provider binary %q not on PATH", binding.Config.Binary)
	}
	return binding, nil
}
