package main

import (
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/supervisor"
	"github.com/jbcom/radioactive-ralph/internal/variant"
	"github.com/jbcom/radioactive-ralph/internal/workspace"
)

// SupervisorCmd is the hidden `radioactive_ralph _supervisor` entry invoked by
// service wrappers (launchd/systemd). Human operators use `radioactive_ralph run`
// instead — which, for now, simply calls through to the same logic.
// This command exists so the plist/systemd unit can target a stable,
// internal-only name that won't get re-pointed at a different code
// path by a future CLI refactor.
type SupervisorCmd struct {
	Variant  string `help:"Variant." required:""`
	RepoRoot string `help:"Repo root." type:"path" required:""`
}

// Run boots a supervisor in the foreground.
func (c *SupervisorCmd) Run(rc *runContext) error {
	p, err := variant.Lookup(c.Variant)
	if err != nil {
		return err
	}
	ws, err := workspace.New(c.RepoRoot, p,
		firstNonEmpty(p.Isolation, variant.IsolationShared),
		firstNonEmptyObj(p.ObjectStoreDefault, variant.ObjectStoreReference),
		firstNonEmptySync(p.SyncSourceDefault, variant.SyncSourceBoth),
		firstNonEmptyLFS(p.LFSModeDefault, variant.LFSOnDemand),
	)
	if err != nil {
		return fmt.Errorf("workspace.New: %w", err)
	}
	sup, err := supervisor.New(supervisor.Options{
		RepoPath:  c.RepoRoot,
		Variant:   p,
		Workspace: ws,
	})
	if err != nil {
		return fmt.Errorf("supervisor.New: %w", err)
	}
	return sup.Run(rc.ctx)
}
