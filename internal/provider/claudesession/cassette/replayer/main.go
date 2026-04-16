// Package main is the cassette replayer binary.
//
// When pointed at a cassette via RALPH_CASSETTE_PATH, it replays the
// recorded stream-json frames to stdout and expects the same stdin
// sequence from the client. This lets tests use claudesession.Session
// exactly as they would with a real `claude -p` subprocess, without
// needing API credentials in CI.
//
// Usage is identical to `claude -p`:
//
//	claudesession.Spawn(ctx, claudesession.Options{
//	    ClaudeBin: ".../replayer",  // built from this package
//	})
//
// The replayer:
//   - honors --session-id and --resume by copying the value into
//     the first "system"/init frame it emits (so s.SessionID()
//     still observes a sensible value in replay mode),
//   - waits for stdin to send each recorded "in" line before
//     emitting the subsequent "out" frames (ensures Send→Reply
//     ordering matches),
//   - falls back to a fast-forward mode (ignoring recorded timing)
//     when RALPH_CASSETTE_FAST=1 is set; otherwise it respects the
//     recorded time offsets within a cap to avoid sleeping forever
//     on slow cassettes.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/provider/claudesession/cassette"
)

const maxSleepPerFrame = 2 * time.Second

func main() {
	path := os.Getenv("RALPH_CASSETTE_PATH")
	if path == "" {
		fmt.Fprintln(os.Stderr, "replayer: RALPH_CASSETTE_PATH unset")
		os.Exit(2)
	}
	c, err := cassette.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "replayer: load %s: %v\n", path, err)
		os.Exit(1)
	}

	sessionID, resumeMode := parseArgs(os.Args[1:])
	fast := os.Getenv("RALPH_CASSETTE_FAST") == "1"

	// stdinCh delivers client-sent lines to the main loop.
	stdinCh := make(chan []byte, 16)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			stdinCh <- line
		}
		close(stdinCh)
	}()

	// Emit frames in order. On "in" frames, wait for stdin to deliver
	// a matching-ish line (same JSON type at minimum); on "out"
	// frames, write to stdout.
	var prev time.Duration
	for i, f := range c.Frames {
		if !fast {
			gap := f.At - prev
			if gap > maxSleepPerFrame {
				gap = maxSleepPerFrame
			}
			if gap > 0 {
				time.Sleep(gap)
			}
		}
		prev = f.At

		switch f.Direction {
		case "out":
			line := maybeRewriteSessionID(f.Line, sessionID, resumeMode, i)
			// Cassettes store frames as indented JSON for readable
			// diffs; compact before emitting so stream-json readers
			// see exactly one frame per line.
			var buf bytes.Buffer
			if err := json.Compact(&buf, line); err != nil {
				_, _ = os.Stdout.Write(line)
			} else {
				_, _ = os.Stdout.Write(buf.Bytes())
			}
			_, _ = os.Stdout.WriteString("\n")
		case "in":
			// Wait for the client to send matching stdin.
			select {
			case _, ok := <-stdinCh:
				if !ok {
					return
				}
			case <-time.After(30 * time.Second):
				fmt.Fprintln(os.Stderr, "replayer: timed out waiting for stdin line")
				os.Exit(3)
			}
		}
	}

	// Drain any remaining client writes before exit so the writer
	// doesn't EPIPE.
	for range stdinCh { //nolint:revive // drain
	}
	wg.Wait()
}

// parseArgs extracts --session-id or --resume from argv.
func parseArgs(args []string) (sessionID string, resume bool) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--session-id":
			if i+1 < len(args) {
				sessionID = args[i+1]
			}
		case "--resume":
			if i+1 < len(args) {
				sessionID = args[i+1]
				resume = true
			}
		}
	}
	return
}

// maybeRewriteSessionID patches the session_id field in the init
// frame so the replayed session carries the ID the client requested
// rather than whatever ID was baked into the cassette.
func maybeRewriteSessionID(line json.RawMessage, id string, resume bool, idx int) []byte {
	if idx != 0 || id == "" {
		return line
	}
	var obj map[string]any
	if err := json.Unmarshal(line, &obj); err != nil {
		return line
	}
	if t, ok := obj["type"].(string); !ok || t != "system" {
		return line
	}
	obj["session_id"] = id
	if resume {
		obj["subtype"] = "resume_ack"
	}
	rewritten, err := json.Marshal(obj)
	if err != nil {
		return line
	}
	return rewritten
}
