package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/fixit"
)

// runAdvisor is the fixit-only code path. It drives the fixit
// six-stage pipeline (see jonbogaty.com/radioactive-ralph/design/)
// and emits a plan file the plans-first discipline can accept.
//
// plansOK reports whether a valid plans/index.md already existed.
// Both --advise (forced advisor) and the "plans missing" fallback
// take this path — the pipeline handles both.
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

	opts := fixit.RunOptions{
		RepoRoot:                repo,
		Topic:                   c.Topic,
		Description:             c.Description,
		NonInteractive:          !interactiveTerminal(),
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

	// Auto-handoff: only when status=current AND no alternate
	// (unambiguous). The spawn path itself lands with M3 session
	// pool; for now we print the command the operator should run.
	if c.AutoHandoff {
		switch {
		case result.Status != fixit.StatusCurrent:
			fmt.Println("radioactive_ralph: --auto-handoff skipped — plan status is not `current`")
		case result.Proposal.Alternate != "":
			fmt.Println("radioactive_ralph: --auto-handoff skipped — recommendation has tradeoffs")
		default:
			fmt.Printf("radioactive_ralph: --auto-handoff → follow-up command:\n  radioactive_ralph run --variant %s --foreground\n",
				result.Proposal.Primary)
		}
	}

	if result.Status == fixit.StatusFallback {
		return fmt.Errorf("fixit emitted a fallback plan — operator intervention required")
	}

	if !plansOK {
		fmt.Println("radioactive_ralph: plans/index.md was missing or malformed; advisor ran as the plans-first fallback.")
	}
	return nil
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
