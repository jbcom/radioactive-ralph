package provider

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeFixtureStillMatchesTheInstalledCLI keeps the captured frame honest.
//
// The unit tests classify a frame recorded from claude 2.1.220. A recorded
// fixture rots silently: if the CLI stops emitting api_error_status, or renames
// it, those tests keep passing while real auth failures go back to being
// generic. This runs the INSTALLED binary against a deliberately invalid key
// and asserts the shape the classifier depends on is still what the CLI emits.
//
// It drives the CLI directly rather than through ClaudeRunner on purpose.
// ClaudeRunner spawns under a pty, and a pty cannot be allocated reliably when
// the test itself runs inside another agent's pty session — a valid,
// fully-authenticated turn fails there identically, so routing through the
// runner would test the harness rather than the CLI contract. What must not
// rot is the FRAME SHAPE, and that is what this checks.
func TestClaudeFixtureStillMatchesTheInstalledCLI(t *testing.T) {
	if os.Getenv("RALPH_REAL_CLI") != "1" {
		t.Skip("set RALPH_REAL_CLI=1 to probe the installed claude binary")
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude not installed")
	}
	dir := t.TempDir()

	cmd := exec.Command(bin,
		"-p", "--input-format", "stream-json",
		"--output-format", "stream-json", "--verbose",
	)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(
		`{"type":"user","message":{"role":"user","content":"hi"}}` + "\n")
	cmd.Env = append(os.Environ(),
		"ANTHROPIC_API_KEY=sk-ant-invalid-probe-key",
		"CLAUDE_CONFIG_DIR="+filepath.Join(dir, "cfg"),
	)
	out, _ := cmd.CombinedOutput() // a rejected key exits nonzero; the frames matter

	var frame claudeResultFrame
	var sawResult bool
	for _, line := range strings.Split(string(out), "\n") {
		var envelope struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &envelope) != nil || envelope.Type != "result" {
			continue
		}
		if json.Unmarshal([]byte(line), &frame) == nil {
			sawResult = true
		}
	}
	if !sawResult {
		t.Skipf("no result frame from the installed CLI (network or CLI change):\n%s", out)
	}

	if !frame.IsError {
		t.Fatalf("installed CLI no longer sets is_error on a rejected key: %+v", frame)
	}
	if frame.APIErrorStatus != 401 {
		t.Fatalf("api_error_status = %d, want 401 — the classifier's structured "+
			"signal has changed shape; update claudeFailureForStatus and the "+
			"captured fixture together", frame.APIErrorStatus)
	}
	if err := frame.failure(); !errors.Is(err, ErrClaudeAuthentication) {
		t.Fatalf("live frame classified as %v, want ErrClaudeAuthentication", err)
	}
}
