package main

import (
	"encoding/json"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// StopCmd is `ralph stop --variant X`.
type StopCmd struct {
	Variant  string `help:"Variant to stop." required:""`
	RepoRoot string `help:"Repo root. Defaults to cwd." type:"path"`
}

// Run asks the supervisor to shut down gracefully.
func (c *StopCmd) Run(rc *runContext) error {
	repo, err := resolveRepoRoot(c.RepoRoot)
	if err != nil {
		return err
	}
	socket, heartbeat, err := socketPath(repo, variant.Name(c.Variant))
	if err != nil {
		return err
	}
	if err := ensureAlive(socket, heartbeat); err != nil {
		return err
	}

	args, err := json.Marshal(ipc.StopArgs{Graceful: true})
	if err != nil {
		return err
	}
	resp, err := roundTrip(rc.ctx, socket, ipc.Request{Cmd: ipc.CmdStop, Args: args})
	if err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("supervisor returned error: %s", resp.Error)
	}
	fmt.Printf("stop requested for variant %s\n", c.Variant)
	return nil
}
