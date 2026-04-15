package main

import (
	"fmt"
	"os/exec"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/service"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/variant"
	"github.com/jbcom/radioactive-ralph/internal/workspace"
)

// RunCmd is `radioactive_ralph run --variant X`.
type RunCmd struct {
	Variant     string  `help:"Variant name (blue, grey, green, red, professor, fixit, immortal, savage, old-man, world-breaker)." required:""`
	Detach      bool    `help:"Spawn the supervisor in a multiplexer pane and return immediately."`
	Foreground  bool    `help:"Run in the foreground — invoked by launchd/systemd service units."`
	RepoRoot    string  `help:"Repo root. Defaults to cwd." type:"path"`
	SpendCapUSD float64 `help:"Spend cap for variants that require one." name:"spend-cap-usd"`

	ConfirmBurnBudget     bool `help:"Confirmation gate for savage." name:"confirm-burn-budget"`
	ConfirmNoMercy        bool `help:"Confirmation gate for old-man." name:"confirm-no-mercy"`
	ConfirmBurnEverything bool `help:"Confirmation gate for world-breaker." name:"confirm-burn-everything"`

	// Fixit-only flags.
	Advise      bool   `help:"(fixit only) Run in advisor mode: scan the codebase, write .radioactive-ralph/plans/<topic>-advisor.md, and sync the first durable DAG plan for this repo. Auto-enabled when no active plan exists for this repo."`
	Topic       string `help:"(fixit --advise only) Slug used for the output filename (plans/<topic>-advisor.md). Defaults to 'general'."`
	Description string `help:"(fixit --advise only) Free-form operator goal. Overrides TOPIC.md. Passed verbatim to the Claude subprocess."`
	AutoHandoff bool   `help:"(fixit --advise only) When the recommendation has no tradeoffs, spawn the recommended variant as a follow-up run automatically."`

	// Advisor refinement thresholds. Operators can also set these in
	// .radioactive-ralph/config.toml under [variants.fixit].
	MaxIterations int    `help:"(fixit --advise only) Max refinement passes. Default 3."`
	MinConfidence int    `help:"(fixit --advise only) Confidence threshold for accepting a proposal without refinement. Default 70."`
	PlanModel     string `help:"(fixit --advise only) Claude model tier for planning. Default opus."`
	PlanEffort    string `help:"(fixit --advise only) Reasoning-effort level for planning (low/medium/high/max). Default high."`
}

// Run launches the supervisor for the named variant.
func (c *RunCmd) Run(rc *runContext) error {
	if c.Detach {
		return fmt.Errorf("--detach is deferred to a follow-up PR; use --foreground for now or run via tmux/screen yourself")
	}

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

	// Gate 1: service-context refusal for unsafe variants.
	if p.SafetyFloors.RefuseServiceContext && service.IsServiceContext() {
		return fmt.Errorf("variant %q refuses to run under launchd/systemd", p.Name)
	}

	// Fixit branch: either --advise explicitly OR plans aren't set up
	// yet. Either way, run the advisor and exit before the supervisor
	// spawn path.
	var plansOK bool
	if p.Name == variant.Fixit {
		plansOK = requireActivePlan(rc.ctx, repo) == nil
		if p.SafetyFloors.RequireSpendCap && !(c.Advise || !plansOK) {
			if spendCap := c.resolveSpendCapUSD(fromConfig); spendCap <= 0 {
				return fmt.Errorf("variant %q requires --spend-cap-usd or [variants.%s] spend_cap_usd", p.Name, p.Name)
			}
		}
		if c.Advise || !plansOK {
			return c.runAdvisor(rc.ctx, repo, plansOK)
		}
	} else {
		if p.SafetyFloors.RequireSpendCap {
			if spendCap := c.resolveSpendCapUSD(fromConfig); spendCap <= 0 {
				return fmt.Errorf("variant %q requires --spend-cap-usd or [variants.%s] spend_cap_usd", p.Name, p.Name)
			}
		}
	}
	if p.Name != variant.Fixit {
		if err := requireActivePlan(rc.ctx, repo); err != nil {
			// Every non-fixit variant refuses without an active plan.
			return err
		}
	}

	// Advise/topic/auto-handoff are fixit-only — reject if set on
	// other variants so the operator can't typo themselves into a
	// silent no-op.
	if p.Name != variant.Fixit && (c.Advise || c.Topic != "" || c.AutoHandoff) {
		return fmt.Errorf("--advise / --topic / --auto-handoff are only valid with --variant fixit")
	}

	// Resolve workspace knobs. Config loads if present; otherwise we
	// fall back to variant defaults.
	// Actual knob-resolution against [variants.X] overrides is wired in
	// M3; M2 scope is enough-to-boot.
	ws, err := workspace.New(repo, p,
		firstNonEmpty(p.Isolation, variant.IsolationShared),
		firstNonEmptyObj(p.ObjectStoreDefault, variant.ObjectStoreReference),
		firstNonEmptySync(p.SyncSourceDefault, variant.SyncSourceBoth),
		firstNonEmptyLFS(p.LFSModeDefault, variant.LFSOnDemand),
	)
	if err != nil {
		return fmt.Errorf("workspace.New: %w", err)
	}

	// Verify claude is installed before spawning the supervisor. This
	// is a cheap, unauthenticated check — it fails fast with a clear
	// error instead of waiting for session spawn to fail later.
	if _, lookErr := exec.LookPath("claude"); lookErr != nil {
		return fmt.Errorf("claude binary not on PATH; install via `npm install -g @anthropic-ai/claude-code`")
	}

	sup, err := supervisor.New(supervisor.Options{
		RepoPath:  repo,
		Variant:   p,
		Workspace: ws,
	})
	if err != nil {
		return fmt.Errorf("supervisor.New: %w", err)
	}

	fmt.Printf("radioactive_ralph: supervisor starting for variant %s in %s\n", p.Name, repo)
	return sup.Run(rc.ctx)
}

// firstNonEmpty picks v if set, else fallback.
func firstNonEmpty(v, fallback variant.IsolationMode) variant.IsolationMode {
	if v == "" {
		return fallback
	}
	return v
}

func firstNonEmptyObj(v, fallback variant.ObjectStoreMode) variant.ObjectStoreMode {
	if v == "" {
		return fallback
	}
	return v
}

func firstNonEmptySync(v, fallback variant.SyncSource) variant.SyncSource {
	if v == "" {
		return fallback
	}
	return v
}

func firstNonEmptyLFS(v, fallback variant.LFSMode) variant.LFSMode {
	if v == "" {
		return fallback
	}
	return v
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
