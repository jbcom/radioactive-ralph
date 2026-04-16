package cassette_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	claudesession "github.com/jbcom/radioactive-ralph/internal/provider/claudesession"
	"github.com/jbcom/radioactive-ralph/internal/provider/claudesession/cassette"
)

// buildBin compiles a helper binary (fakeclaude or replayer) and
// returns its path.
func buildBin(t *testing.T, pkg string) string {
	t.Helper()
	dir := t.TempDir()
	name := filepath.Base(pkg)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", bin, pkg)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", pkg, err, out)
	}
	return bin
}

// TestRecordThenReplayRoundTrip records a conversation against the
// fake-claude test double, saves the cassette, then replays it
// through claudesession.Spawn and verifies the client observes the same
// frames.
func TestRecordThenReplayRoundTrip(t *testing.T) {
	fake := buildBin(t, "github.com/jbcom/radioactive-ralph/internal/provider/claudesession/internal/fakeclaude")
	replayer := buildBin(t, "github.com/jbcom/radioactive-ralph/internal/provider/claudesession/cassette/replayer")
	cassettePath := filepath.Join(t.TempDir(), "cassette.json")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// ── Phase 1: RECORD ──────────────────────────────────────────
	rec, err := cassette.NewRecorder(ctx, cassettePath, fake,
		[]string{"--session-id", "test-sid-001"})
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}
	if err := rec.Start(); err != nil {
		t.Fatalf("rec.Start: %v", err)
	}

	// Send one user message, drain assistant+result frames.
	_, _ = rec.Stdin().Write([]byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello record"}]}}` + "\n"))

	// Read until we see a result frame.
	dec := json.NewDecoder(rec.Stdout())
	var sawResult bool
	deadline := time.After(5 * time.Second)
	for !sawResult {
		var frame map[string]any
		errCh := make(chan error, 1)
		go func() { errCh <- dec.Decode(&frame) }()
		select {
		case <-deadline:
			t.Fatal("no result frame within 5s during record")
		case err := <-errCh:
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if t, _ := frame["type"].(string); t == "result" {
				sawResult = true
			}
		}
	}

	if err := rec.Close(); err != nil {
		t.Fatalf("rec.Close: %v", err)
	}

	// Cassette on disk should be non-empty and include at least one
	// "in" and one "out" frame.
	c, err := cassette.Load(cassettePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var sawIn, sawOut bool
	for _, f := range c.Frames {
		switch f.Direction {
		case "in":
			sawIn = true
		case "out":
			sawOut = true
		}
	}
	if !sawIn {
		t.Error("cassette missing any 'in' frame")
	}
	if !sawOut {
		t.Error("cassette missing any 'out' frame")
	}

	// ── Phase 2: REPLAY via claudesession.Spawn ───────────────────
	t.Setenv("RALPH_CASSETTE_PATH", cassettePath)
	t.Setenv("RALPH_CASSETTE_FAST", "1") // skip timing delays in CI
	s, err := claudesession.Spawn(ctx, claudesession.Options{
		ClaudeBin:  replayer,
		WorkingDir: t.TempDir(),
		SessionID:  "test-sid-001", // match the recorded value
	})
	if err != nil {
		t.Fatalf("Spawn(replayer): %v", err)
	}
	defer func() { _ = s.Close() }()

	// Send the same user message; replayer should emit the recorded
	// assistant + result frames.
	if err := s.SendUserMessage(ctx, "hello record"); err != nil {
		t.Fatalf("SendUserMessage: %v", err)
	}

	// Collect replayed events.
	sawResult = false
	sawAssistant := false
	drainDeadline := time.After(10 * time.Second)
	for !sawResult || !sawAssistant {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatalf("events closed too early (result=%v assistant=%v)", sawResult, sawAssistant)
			}
			if ev.Err != nil {
				t.Fatalf("event err: %v", ev.Err)
			}
			switch ev.Inbound.Type {
			case "assistant":
				sawAssistant = true
			case "result":
				sawResult = true
			}
		case <-drainDeadline:
			t.Fatalf("timeout during replay; assistant=%v result=%v", sawAssistant, sawResult)
		}
	}
}

// TestCassetteLoadRejectsMalformedFile confirms cassette.Load
// surfaces a useful error for corrupted JSON.
func TestCassetteLoadRejectsMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("not json at all"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := cassette.Load(path); err == nil {
		t.Fatal("expected error on malformed cassette")
	}
}

// TestCassetteSaveProducesIndentedJSON verifies the on-disk format is
// human-readable (important for review diffs).
func TestCassetteSaveProducesIndentedJSON(t *testing.T) {
	c := &cassette.Cassette{
		Version:    cassette.CurrentVersion,
		RecordedAt: time.Now().UTC(),
		Frames: []cassette.Frame{
			{Direction: "out", At: 0, Line: json.RawMessage(`{"type":"system"}`)},
		},
	}
	path := filepath.Join(t.TempDir(), "out.json")
	if err := c.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(raw), "\n  ") {
		t.Errorf("expected indented JSON, got:\n%s", raw)
	}
}
