package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestRunSupervisorModeJSONLogFormat confirms --log-format json actually
// changes the supervisor's stderr output shape to rlog's stream-json
// records (type=ralph), not the default human-readable text — this is the
// wiring internal/rlog exists for for observability during the E2E harness
// and in operator-facing log tailing.
func TestRunSupervisorModeJSONLogFormat(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runSupervisorMode(ctx, "json")
	}()

	lineCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(r)
		if scanner.Scan() {
			lineCh <- scanner.Text()
		}
	}()

	var firstLine string
	select {
	case firstLine = <-lineCh:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("timed out waiting for a stderr line from the JSON-mode supervisor")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not exit within 3s of ctx cancel")
	}
	_ = w.Close()

	if !strings.HasPrefix(strings.TrimSpace(firstLine), "{") {
		t.Fatalf("expected a JSON line in --log-format json mode, got: %q", firstLine)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(firstLine), &rec); err != nil {
		t.Fatalf("unmarshal stderr line %q: %v", firstLine, err)
	}
	if rec["type"] != "ralph" {
		t.Errorf(`rec["type"] = %v, want "ralph"`, rec["type"])
	}
	if rec["event"] != "supervisor.starting" {
		t.Errorf(`rec["event"] = %v, want "supervisor.starting"`, rec["event"])
	}
}
