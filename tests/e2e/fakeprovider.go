package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// WriteFakeClaudeCLI writes a small script named "claude" (or
// "claude.bat"/"claude.cmd" is NOT needed here — e2e only runs on
// Unix-like CI runners for the pty-driven flow) into dir that mimics just
// enough of the real `claude -p --input-format stream-json --output-format
// stream-json` contract for internal/provider.ClaudeRunner to parse a
// terminal result frame: it reads one line of stdin (the stream-json user
// message ClaudeRunner writes) and discards it, then emits a single
// assistant frame followed by a `type":"result"` frame with a fixed,
// near-zero cost/usage so the orchestrator's spend accounting sees a real
// (tiny) number rather than a hardcoded zero. This lets the CI-feasible
// E2E flow drive a REAL subprocess through agent.Start/agent.Watch — the
// full pty-owning, watchdog-supervised path — without spending against a
// real hosted model.
//
// Returns dir; callers typically join filepath.Join(dir, "claude") to get
// an absolute path to pass as a provider.BindingConfig.Binary, rather than
// relying on PATH lookup.
func WriteFakeClaudeCLI(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake provider CLI script is a POSIX shell script; not supported on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "claude")

	script := `#!/bin/sh
# Fake claude CLI for the CI-feasible E2E flow (tests/e2e). Reads and
# discards one line of stdin (the stream-json user message the real
# ClaudeRunner writes), then emits a canned assistant + result frame pair
# matching internal/provider.parseClaudeStreamLine's expected shape.
read -r _line
echo '{"type":"assistant","message":{"content":[{"type":"text","text":"fake E2E turn complete"}]}}'
echo '{"type":"result","total_cost_usd":0.0001,"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}'
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test-only fake CLI must be executable
		t.Fatalf("write fake claude CLI: %v", err)
	}
	return dir
}
