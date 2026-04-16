package main

import (
	"fmt"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

// StopCmd is `radioactive_ralph stop`.
type StopCmd struct {
	RepoRoot string `help:"Repo root. Defaults to cwd." type:"path"`
}

// Run asks the repo service to shut down gracefully.
func (c *StopCmd) Run(rc *runContext) error {
	repo, err := resolveRepoRoot(c.RepoRoot)
	if err != nil {
		return err
	}
	socket, heartbeat, err := socketPath(repo)
	if err != nil {
		return err
	}
	if err := ensureAlive(socket, heartbeat); err != nil {
		return err
	}
	client, err := ipc.Dial(socket, 5*time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	err = client.Stop(rc.ctx, ipc.StopArgs{Graceful: true})
	if err != nil {
		return err
	}
	fmt.Printf("stop requested for repo service in %s\n", repo)
	return nil
}
