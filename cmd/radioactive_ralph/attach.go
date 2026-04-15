package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// AttachCmd is `radioactive_ralph attach --variant X`.
type AttachCmd struct {
	Variant  string `help:"Variant to attach to." required:""`
	RepoRoot string `help:"Repo root. Defaults to cwd." type:"path"`
	Raw      bool   `help:"Emit raw JSON-line event frames instead of the default one-line summary."`
}

// Run streams events from the supervisor until the operator cancels.
func (c *AttachCmd) Run(rc *runContext) error {
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

	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(rc.ctx, "unix", socket)
	if err != nil {
		return fmt.Errorf("dial %s: %w", socket, err)
	}
	defer func() { _ = conn.Close() }()

	req, err := json.Marshal(ipc.Request{Cmd: ipc.CmdAttach})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	// Close the conn when ctx fires to unblock the scanner.
	go func() {
		<-rc.ctx.Done()
		_ = conn.Close()
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if c.Raw {
			fmt.Println(string(line))
			continue
		}
		// The supervisor sends {"event": {...}} frames for stream mode
		// and a final {"ok": true} to close. Parse either.
		var frame ipc.StreamEvent
		if err := json.Unmarshal(line, &frame); err == nil && len(frame.Event) > 0 {
			printEvent(frame.Event)
			continue
		}
		var resp ipc.Response
		if err := json.Unmarshal(line, &resp); err == nil {
			if !resp.Ok {
				return fmt.Errorf("supervisor returned error: %s", resp.Error)
			}
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
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
