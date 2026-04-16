package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

// StatusCmd is `radioactive_ralph status`.
type StatusCmd struct {
	RepoRoot string `help:"Repo root. Defaults to cwd." type:"path"`
	JSON     bool   `help:"Emit status as JSON instead of the default text table."`
}

// Run dials the repo service socket and prints the status reply.
func (c *StatusCmd) Run(rc *runContext) error {
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
	status, err := client.Status(rc.ctx)
	if err != nil {
		return err
	}
	if c.JSON {
		enc := json.NewEncoder(newStdoutWriter())
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	fmt.Printf("repo:             %s\n", status.RepoPath)
	fmt.Printf("pid:              %d\n", status.PID)
	fmt.Printf("uptime:           %s\n", status.Uptime.Round(time.Second))
	fmt.Printf("active_plans:     %d\n", status.ActivePlans)
	fmt.Printf("ready_tasks:      %d\n", status.ReadyTasks)
	fmt.Printf("approval_tasks:   %d\n", status.ApprovalTasks)
	fmt.Printf("blocked_tasks:    %d\n", status.BlockedTasks)
	fmt.Printf("active_workers:   %d\n", status.ActiveWorkers)
	fmt.Printf("running_tasks:    %d\n", status.RunningTasks)
	fmt.Printf("failed_tasks:     %d\n", status.FailedTasks)
	if !status.LastEventAt.IsZero() {
		fmt.Printf("last_event:       %s\n", status.LastEventAt.Format(time.RFC3339))
	}
	return nil
}
