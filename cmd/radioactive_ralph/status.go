package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// StatusCmd is `radioactive_ralph status --variant X`.
type StatusCmd struct {
	Variant  string `help:"Variant to query." required:""`
	RepoRoot string `help:"Repo root. Defaults to cwd." type:"path"`
	JSON     bool   `help:"Emit status as JSON instead of the default text table."`
}

// Run dials the supervisor socket and prints the status reply.
func (c *StatusCmd) Run(rc *runContext) error {
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

	resp, err := roundTrip(rc.ctx, socket, ipc.Request{Cmd: ipc.CmdStatus})
	if err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("supervisor returned error: %s", resp.Error)
	}

	var status ipc.StatusReply
	if err := json.Unmarshal(resp.Data, &status); err != nil {
		return fmt.Errorf("decode status: %w", err)
	}
	if c.JSON {
		enc := json.NewEncoder(newStdoutWriter())
		enc.SetIndent("", "  ")
		return enc.Encode(status)
	}

	fmt.Printf("variant:          %s\n", status.Variant)
	fmt.Printf("pid:              %d\n", status.PID)
	fmt.Printf("uptime:           %s\n", status.Uptime.Round(time.Second))
	fmt.Printf("active_sessions:  %d\n", status.ActiveSessions)
	fmt.Printf("queued_tasks:     %d\n", status.QueuedTasks)
	fmt.Printf("running_tasks:    %d\n", status.RunningTasks)
	if !status.LastEventAt.IsZero() {
		fmt.Printf("last_event:       %s\n", status.LastEventAt.Format(time.RFC3339))
	}
	return nil
}
