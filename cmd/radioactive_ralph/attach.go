package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

// AttachCmd is `radioactive_ralph attach`.
type AttachCmd struct {
	RepoRoot string `help:"Repo root. Defaults to cwd." type:"path"`
	Raw      bool   `help:"Emit raw JSON-line event frames instead of the default one-line summary."`
}

// Run streams events from the repo service until the operator cancels.
func (c *AttachCmd) Run(rc *runContext) error {
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
	return client.Attach(rc.ctx, func(raw json.RawMessage) error {
		if c.Raw {
			fmt.Println(string(raw))
			return nil
		}
		printEvent(raw)
		return nil
	})
}

// printEvent prints a one-line digest of a single event. Full payload
// is in --raw.
func printEvent(raw json.RawMessage) {
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		fmt.Fprintln(os.Stderr, "attach: malformed event:", err)
		return
	}
	ts, _ := parsed["timestamp"].(string)
	kind, _ := parsed["kind"].(string)
	actor, _ := parsed["actor"].(string)
	fmt.Printf("%s  %-25s  %s\n", ts, kind, actor)
}
