package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/plandag"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	runtimecmd "github.com/jbcom/radioactive-ralph/internal/runtime"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// RunCmd is `radioactive_ralph run --variant X`.
type RunCmd struct {
	Variant     string  `help:"Variant name (blue, grey, green, red, professor, fixit, immortal, savage, old-man, world-breaker)." required:""`
	RepoRoot    string  `help:"Repo root. Defaults to cwd." type:"path"`
	SpendCapUSD float64 `help:"Spend cap for variants that require one." name:"spend-cap-usd"`

	ConfirmBurnBudget     bool `help:"Confirmation gate for savage." name:"confirm-burn-budget"`
	ConfirmNoMercy        bool `help:"Confirmation gate for old-man." name:"confirm-no-mercy"`
	ConfirmBurnEverything bool `help:"Confirmation gate for world-breaker." name:"confirm-burn-everything"`

	// Fixit-only flags.
	Advise      bool   `help:"(fixit only) Run in advisor mode: scan the codebase, write .radioactive-ralph/plans/<topic>-advisor.md, and sync the first durable DAG plan for this repo. Auto-enabled when no active plan exists for this repo."`
	Topic       string `help:"(fixit --advise only) Slug used for the output filename (plans/<topic>-advisor.md). Defaults to 'general'."`
	Description string `help:"(fixit --advise only) Free-form operator goal. Overrides TOPIC.md. Passed verbatim to the provider subprocess."`
	AutoHandoff bool   `help:"(fixit --advise only) When the recommendation has no tradeoffs, start the recommended variant automatically."`

	// Advisor refinement thresholds. Operators can also set these in
	// .radioactive-ralph/config.toml under [variants.fixit].
	MaxIterations int    `help:"(fixit --advise only) Max refinement passes. Default 3."`
	MinConfidence int    `help:"(fixit --advise only) Confidence threshold for accepting a proposal without refinement. Default 70."`
	PlanModel     string `help:"(fixit --advise only) Provider model tier for planning. Default opus."`
	PlanEffort    string `help:"(fixit --advise only) Reasoning-effort level for planning (low/medium/high/max). Default high."`
}

// Run executes one bounded variant attached to the current terminal.
func (c *RunCmd) Run(rc *runContext) error {
	repo, err := resolveRepoRoot(c.RepoRoot)
	if err != nil {
		return err
	}

	p, err := variant.Lookup(c.Variant)
	if err != nil {
		return err
	}

	cfg, err := config.Load(repo)
	if err != nil && !config.IsMissingConfig(err) {
		return fmt.Errorf("config.Load: %w", err)
	}
	var fromConfig config.VariantFile
	if err == nil && cfg.Variants != nil {
		fromConfig = cfg.Variants[string(p.Name)]
	}

	if p.HasGate() && !c.gateConfirmed(p) {
		return fmt.Errorf("variant %q requires %s", p.Name, p.ConfirmationGate)
	}
	if p.Name != variant.Fixit && (c.Advise || c.Topic != "" || c.AutoHandoff) {
		return fmt.Errorf("--advise / --topic / --auto-handoff are only valid with --variant fixit")
	}
	if !p.AttachedAllowed {
		return fmt.Errorf("variant %q requires the durable repo service; use `radioactive_ralph service start`", p.Name)
	}

	// Refuse to compete with an already-running durable repo service.
	socket, heartbeat, sockErr := socketPath(repo)
	if sockErr != nil {
		return sockErr
	}
	if _, err := os.Stat(socket); err == nil {
		if err := ensureAlive(socket, heartbeat); err == nil {
			return fmt.Errorf("repo service already running for %s; use `radioactive_ralph attach`, `status`, or `stop` instead of spawning a competing attached run", repo)
		}
		return fmt.Errorf("repo service socket exists for %s but is stale; remove %s and %s or rerun `radioactive_ralph service start`", repo, socket, heartbeat)
	}

	var plansOK bool
	if p.Name == variant.Fixit {
		plansOK = requireActivePlan(rc.ctx, repo) == nil
		if p.SafetyFloors.RequireSpendCap && !c.Advise && plansOK {
			if spendCap := c.resolveSpendCapUSD(fromConfig); spendCap <= 0 {
				return fmt.Errorf("variant %q requires --spend-cap-usd or [variants.%s] spend_cap_usd", p.Name, p.Name)
			}
		}
		if c.Advise || !plansOK {
			return c.runAdvisor(rc.ctx, repo, plansOK)
		}
	} else {
		if err := requireActivePlan(rc.ctx, repo); err != nil {
			return err
		}
		if p.SafetyFloors.RequireSpendCap {
			if spendCap := c.resolveSpendCapUSD(fromConfig); spendCap <= 0 {
				return fmt.Errorf("variant %q requires --spend-cap-usd or [variants.%s] spend_cap_usd", p.Name, p.Name)
			}
		}
	}

	if err := verifyProviderAvailable(cfg, repo, p, fromConfig); err != nil {
		return err
	}

	svc, err := runtimecmd.NewService(runtimecmd.Options{
		RepoPath:         repo,
		SessionMode:      plandag.SessionModeAttached,
		SessionTransport: plandag.SessionTransportStdio,
		VariantFilter:    p.Name,
		ExitWhenIdle:     true,
	})
	if err != nil {
		return fmt.Errorf("runtime.NewService: %w", err)
	}

	fmt.Printf("radioactive_ralph: attached run starting for %s in %s\n", p.Name, repo)
	return svc.Run(rc.ctx)
}

func (c *RunCmd) gateConfirmed(p variant.Profile) bool {
	switch p.ConfirmationGate {
	case "--confirm-burn-budget":
		return c.ConfirmBurnBudget
	case "--confirm-no-mercy":
		return c.ConfirmNoMercy
	case "--confirm-burn-everything":
		return c.ConfirmBurnEverything
	default:
		return true
	}
}

func (c *RunCmd) resolveSpendCapUSD(fromConfig config.VariantFile) float64 {
	if c.SpendCapUSD > 0 {
		return c.SpendCapUSD
	}
	if fromConfig.SpendCapUSD != nil {
		return *fromConfig.SpendCapUSD
	}
	return 0
}

func verifyProviderAvailable(cfg config.File, repo string, p variant.Profile, fromConfig config.VariantFile) error {
	local, err := config.LoadLocal(repo)
	if err != nil && !config.IsMissingLocal(err) {
		return fmt.Errorf("config.LoadLocal: %w", err)
	}
	binding, err := provider.ResolveBinding(cfg, local, p, fromConfig)
	if err != nil {
		return err
	}
	if binding.Config.Binary == "" {
		return fmt.Errorf("provider %q has no configured binary", binding.Name)
	}
	if err := provider.ValidateBinding(binding); err != nil {
		return err
	}
	if _, err := exec.LookPath(binding.Config.Binary); err != nil {
		return fmt.Errorf("provider binary %q not on PATH", binding.Config.Binary)
	}
	return nil
}
