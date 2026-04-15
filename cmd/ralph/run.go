package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/service"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/variant"
	"github.com/jbcom/radioactive-ralph/internal/workspace"
)

// RunCmd is `ralph run --variant X`.
type RunCmd struct {
	Variant    string `help:"Variant name (blue, grey, green, red, professor, fixit, immortal, savage, old-man, world-breaker)." required:""`
	Detach     bool   `help:"Spawn the supervisor in a multiplexer pane and return immediately."`
	Foreground bool   `help:"Run in the foreground — invoked by launchd/systemd service units."`
	RepoRoot   string `help:"Repo root. Defaults to cwd." type:"path"`
}

// Run launches the supervisor for the named variant.
//
// M2 behavior (this pass):
//   - --foreground: directly runs supervisor.Run in the current process.
//     This is the path invoked by launchd/systemd service units.
//   - neither flag: same as --foreground for now; multiplexer detach is
//     deferred to a follow-up that wires internal/multiplexer
//     end-to-end with supervisor exec.
//   - --detach: rejected with a clear "not yet" message rather than
//     silently acting like --foreground.
func (c *RunCmd) Run(rc *runContext) error {
	if c.Detach {
		return fmt.Errorf("--detach is deferred to a follow-up PR; use --foreground for now or run via tmux/screen yourself")
	}

	repo := c.RepoRoot
	if repo == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("cwd: %w", err)
		}
		repo = cwd
	}

	p, err := variant.Lookup(c.Variant)
	if err != nil {
		return err
	}

	// Gate 1: service-context refusal for unsafe variants.
	if p.SafetyFloors.RefuseServiceContext && service.IsServiceContext() {
		return fmt.Errorf("variant %q refuses to run under launchd/systemd", p.Name)
	}

	// Gate 2: plans-first discipline — non-fixit variants require plans/index.md.
	if p.Name != variant.Fixit {
		if err := requirePlansIndex(repo); err != nil {
			return err
		}
	}

	// Resolve workspace knobs. Config loads if present; otherwise we
	// fall back to variant defaults.
	if _, err := config.Load(repo); err != nil && !config.IsMissingConfig(err) {
		return fmt.Errorf("config.Load: %w", err)
	}
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

	fmt.Printf("ralph: supervisor starting for variant %s in %s\n", p.Name, repo)
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
