package provider

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agent"
)

func TestClaudeRunnerNaturalNonzeroDominatesSuccessFrame(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-claude-success-then-nonzero.sh", `#!/bin/sh
IFS= read -r _
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"PARTIAL-CLAUDE-SUCCESS-SECRET"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"RESULT-SECRET"}'
if IFS= read -r _; then
  exit 28
fi
exit 27
`)
	result, err := ClaudeRunner{}.Run(context.Background(), Binding{
		Name:   "claude",
		Config: BindingConfig{Type: "claude", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "claude exited nonzero") {
		t.Fatalf("Run error = %v, want natural nonzero failure", err)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no partial result", result)
	}
	for _, secret := range []string{"PARTIAL-CLAUDE-SUCCESS-SECRET", "RESULT-SECRET"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error surfaced provider payload %q: %v", secret, err)
		}
	}
}

func TestClaudeRunnerClosesOneShotInputBeforeAwaitingNaturalExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-claude-waits-for-eof.sh", `#!/bin/sh
count=0
while IFS= read -r _; do
  count=$((count + 1))
done
if [ "$count" -ne 1 ]; then
  exit 29
fi
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"EOF-DELIVERED"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false}'
`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := ClaudeRunner{}.Run(ctx, Binding{
		Name:   "claude",
		Config: BindingConfig{Type: "claude", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
	if err != nil {
		t.Fatalf("Run error = %v, want natural success after stdin EOF", err)
	}
	if result.AssistantOutput != "EOF-DELIVERED" {
		t.Fatalf("AssistantOutput = %q, want EOF-DELIVERED", result.AssistantOutput)
	}
}

func TestOpencodeRunnerErrorFrameTerminatesEndlessTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-opencode-error-endless-tail.sh", `#!/bin/sh
printf '%s\n' '{"type":"error","sessionID":"ses_error","error":{"name":"ERROR-NAME-SECRET","data":{"message":"ERROR-MESSAGE-SECRET"}}}'
while :; do
  printf '%s\n' 'ENDLESS-TAIL-SECRET'
done
`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	result, err := OpencodeRunner{}.Run(ctx, Binding{
		Name:   "opencode",
		Config: BindingConfig{Type: "opencode", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
	if !errors.Is(err, ErrOpencodeReportedError) {
		t.Fatalf("Run error = %v, want ErrOpencodeReportedError", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error frame did not terminate the endless tail: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("error-frame termination took %s, want bounded prompt return", elapsed)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no partial result", result)
	}
	for _, secret := range []string{"ERROR-NAME-SECRET", "ERROR-MESSAGE-SECRET", "ENDLESS-TAIL-SECRET"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error surfaced provider payload %q: %v", secret, err)
		}
	}
}

func TestOpencodeRunnerBoundsEndlessNonJSONProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-opencode-endless-noise.sh", `#!/bin/sh
printf '%s\n' '{"type":"text","sessionID":"ses_partial","part":{"type":"text","text":"OPENCODE-PARTIAL-SECRET"}}'
python3 -u - <<'PY'
import sys
line = b"x" * ((1 << 20) - 1) + b"\n"
while True:
    sys.stdout.buffer.write(line)
    sys.stdout.buffer.flush()
PY
`)
	ctx, cancel := context.WithTimeout(context.Background(), endlessOutputBudget)
	defer cancel()
	result, err := OpencodeRunner{}.Run(ctx, Binding{
		Name:   "opencode",
		Config: BindingConfig{Type: "opencode", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
	if !errors.Is(err, agent.ErrObservedOutputTooLarge) {
		t.Fatalf("Run error = %v, want agent.ErrObservedOutputTooLarge", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("endless non-JSON progress escaped the byte bound: %v", err)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no partial result", result)
	}
	if strings.Contains(err.Error(), "OPENCODE-PARTIAL-SECRET") {
		t.Fatalf("raw ceiling error surfaced partial result: %v", err)
	}
}

func TestClaudeRunnerBoundsEndlessNonJSONProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-claude-endless-noise.sh", `#!/bin/sh
IFS= read -r _
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"CLAUDE-PARTIAL-SECRET"}]}}'
python3 -u - <<'PY'
import sys
# 64 KiB exercises non-JSON streaming without making the race detector spend
# quadratic time copying a megabyte-sized partial PTY record before each
# newline. The cumulative 16 MiB ceiling remains the behavior under test.
line = b"x" * ((1 << 16) - 1) + b"\n"
while True:
    sys.stdout.buffer.write(line)
    sys.stdout.buffer.flush()
PY
`)
	ctx, cancel := context.WithTimeout(context.Background(), endlessOutputBudget)
	defer cancel()
	result, err := ClaudeRunner{}.Run(ctx, Binding{
		Name:   "claude",
		Config: BindingConfig{Type: "claude", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
	if !errors.Is(err, agent.ErrObservedOutputTooLarge) {
		t.Fatalf("Run error = %v, want agent.ErrObservedOutputTooLarge", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("endless non-JSON progress escaped the byte bound: %v", err)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no partial result", result)
	}
	if strings.Contains(err.Error(), "CLAUDE-PARTIAL-SECRET") {
		t.Fatalf("raw ceiling error surfaced partial result: %v", err)
	}
}

func TestCodexRunnerBoundsEndlessObservationalProgress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex-endless-progress.sh", `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then out="$2"; shift 2; else shift; fi
done
printf '%s' 'CODEX-LAST-MESSAGE-PARTIAL-SECRET' > "$out"
python3 -u - <<'PY'
import sys
line = b"x" * ((1 << 20) - 1) + b"\n"
while True:
    sys.stdout.buffer.write(line)
    sys.stdout.buffer.flush()
PY
`)
	ctx, cancel := context.WithTimeout(context.Background(), endlessOutputBudget)
	defer cancel()
	result, err := CodexRunner{}.Run(ctx, Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if !errors.Is(err, agent.ErrObservedOutputTooLarge) {
		t.Fatalf("Run error = %v, want agent.ErrObservedOutputTooLarge", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("endless observational progress escaped the byte bound: %v", err)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no partial result", result)
	}
	if strings.Contains(err.Error(), "CODEX-LAST-MESSAGE-PARTIAL-SECRET") {
		t.Fatalf("raw ceiling error surfaced partial result: %v", err)
	}
}

func TestCodexRunnerTurnFailedDominatesExitZeroAndLastMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	const lastMessageSecret = "LAST-MESSAGE-SHOULD-NOT-CROSS"
	const providerErrorSecret = "TURN-FAILED-PROVIDER-SECRET"
	bin := fakeCodexCLI(
		t,
		"fake-codex-turn-failed-exit-zero.sh",
		lastMessageSecret,
		`{"type":"turn.failed","error":{"message":"quota exhausted `+providerErrorSecret+`"}}`,
		0,
	)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if !errors.Is(err, ErrCodexTurnFailed) {
		t.Fatalf("Run error = %v, want ErrCodexTurnFailed", err)
	}
	if !strings.Contains(err.Error(), codexFailureQuota.String()) {
		t.Fatalf("Run error = %v, want static quota category", err)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no last-message result", result)
	}
	for _, secret := range []string{lastMessageSecret, providerErrorSecret} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error surfaced provider payload %q: %v", secret, err)
		}
	}
}

func TestCodexRunnerTurnFailedTerminatesEndlessTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex-turn-failed-endless-tail.sh", `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then out="$arg"; fi
  previous="$arg"
done
printf '%s' 'FORGED-ENDLESS-SUCCESS' > "$out"
printf '%s\n' '{"type":"turn.failed","error":{"message":"quota exhausted ENDLESS-FAILURE-SECRET"}}'
sleep 300
`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	result, err := CodexRunner{}.Run(ctx, Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if !errors.Is(err, ErrCodexTurnFailed) {
		t.Fatalf("Run error = %v, want ErrCodexTurnFailed", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("turn.failed did not terminate the endless tail: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("turn.failed convergence took %s, want bounded return", elapsed)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no forged result", result)
	}
	for _, secret := range []string{"FORGED-ENDLESS-SUCCESS", "ENDLESS-FAILURE-SECRET"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error surfaced provider payload %q: %v", secret, err)
		}
	}
}

func TestCodexRunnerOversizedTurnFailedDominatesExitZeroAndLastMessage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex-oversized-turn-failed.sh", `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then out="$arg"; fi
  previous="$arg"
done
printf '%s' 'FORGED-OVERSIZED-SUCCESS' > "$out"
python3 - <<'PY'
import json, sys
event = {"type": "turn.failed", "error": {"message": "OVERSIZED-FAILURE-SECRET" + "x" * (96 << 10)}}
json.dump(event, sys.stdout, separators=(",", ":"))
sys.stdout.write("\n")
PY
`)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if !errors.Is(err, ErrCodexTurnFailed) {
		t.Fatalf("Run error = %v, want ErrCodexTurnFailed", err)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no forged last-message result", result)
	}
	for _, secret := range []string{"FORGED-OVERSIZED-SUCCESS", "OVERSIZED-FAILURE-SECRET"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error surfaced provider payload %q: %v", secret, err)
		}
	}
}

func TestCodexRunnerLargeRetainedCommandTerminalLiteralRemainsSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex-oversized-command-marker.sh", `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then out="$arg"; fi
  previous="$arg"
done
printf '%s' 'oversized command accepted' > "$out"
python3 - <<'PY'
import json, sys
event = {
    "padding": "reordered benign event",
    "type": "item.completed",
    "item": {
        "type": "command_execution",
        "aggregated_output": '{"type":"turn.failed"}' + "x" * (96 << 10),
    },
}
json.dump(event, sys.stdout, separators=(",", ":"))
sys.stdout.write("\n")
PY
`)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Run rejected clean oversized command event: %v", err)
	}
	if result.AssistantOutput != "oversized command accepted" {
		t.Fatalf("AssistantOutput = %q, want authoritative success", result.AssistantOutput)
	}
}

func TestCodexRunnerLargeRetainedReorderedFailureRemainsAuthoritative(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex-oversized-reordered.sh", `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then out="$arg"; fi
  previous="$arg"
done
printf '%s' 'FORGED-REORDERED-SUCCESS' > "$out"
python3 - <<'PY'
import json, sys
event = {
    "padding": "x" * (96 << 10),
    "type": "turn.failed",
    "error": {"message": "REORDERED-FAILURE-SECRET"},
}
json.dump(event, sys.stdout, separators=(",", ":"))
sys.stdout.write("\n")
PY
`)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if !errors.Is(err, ErrCodexTurnFailed) {
		t.Fatalf("Run error = %v, want ErrCodexTurnFailed", err)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no forged last-message result", result)
	}
	for _, secret := range []string{"FORGED-REORDERED-SUCCESS", "REORDERED-FAILURE-SECRET"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error surfaced provider payload %q: %v", secret, err)
		}
	}
}

func TestCodexDiscardedPrefixSchemaGuard(t *testing.T) {
	tests := []struct {
		name       string
		prefix     string
		wantErr    error
		wantFailed bool
	}{
		{
			name:       "reordered structured event fails closed",
			prefix:     `{"padding":"still-open","type":"turn.failed"}`,
			wantErr:    ErrCodexOversizeSchema,
			wantFailed: true,
		},
		{
			name:       "first key beyond prefix fails closed",
			prefix:     `{"` + strings.Repeat("p", maxDiscardedOutputPrefixTestBytes),
			wantErr:    ErrCodexOversizeSchema,
			wantFailed: true,
		},
		{
			name:       "empty object with unseen tail fails closed",
			prefix:     `{}` + strings.Repeat("x", maxDiscardedOutputPrefixTestBytes-2),
			wantErr:    ErrCodexOversizeSchema,
			wantFailed: true,
		},
		{
			name:   "malformed pane noise stays non-authoritative",
			prefix: `not-json "type":"turn.failed"`,
		},
		{
			name:       "type-first nonterminal object fails closed",
			prefix:     `{"type":"item.completed","item":{"text":"type turn.failed"}}`,
			wantErr:    ErrCodexOversizeSchema,
			wantFailed: true,
		},
		{
			name:       "all whitespace is inconclusive and fails closed",
			prefix:     strings.Repeat(" ", maxDiscardedOutputPrefixTestBytes),
			wantErr:    ErrCodexOversizeSchema,
			wantFailed: true,
		},
		{
			name:   "positive non-object proof remains pane noise",
			prefix: " \t\r\nnot-json \"type\":\"turn.failed\"",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var diagnostics codexDiagnosticCollector
			done := diagnostics.consumeDiscardedPrefix([]byte(tc.prefix))
			if done != tc.wantFailed {
				t.Fatalf("terminal = %v, want %v", done, tc.wantFailed)
			}
			if !errors.Is(diagnostics.failure(), tc.wantErr) {
				t.Fatalf("failure = %v, want %v", diagnostics.failure(), tc.wantErr)
			}
		})
	}
}

const maxDiscardedOutputPrefixTestBytes = 4 << 10

func TestCodexDiagnosticCollectorDetectsTurnFailedAfterDiagnosticExhaustion(t *testing.T) {
	var diagnostics codexDiagnosticCollector
	for range maxCodexDiagnosticFrames + 1 {
		diagnostics.consume([]byte(`{"type":"thread.started"}`))
	}
	if !diagnostics.exhausted {
		t.Fatal("setup did not exhaust diagnostic collection")
	}
	diagnostics.consume([]byte(
		`{"type":"turn.failed","error":{"message":"TURN-FAILED-AFTER-BUDGET-SECRET"}}`,
	))
	if !diagnostics.failed() {
		t.Fatal("turn.failed was hidden behind earlier diagnostic-budget exhaustion")
	}
	if got := diagnostics.String(); got != codexFailureGeneric.String() {
		t.Fatalf("exhausted diagnostics changed to %q, want static generic category", got)
	}
	if strings.Contains(diagnostics.String(), "TURN-FAILED-AFTER-BUDGET-SECRET") {
		t.Fatal("provider message crossed the diagnostic boundary")
	}
}

func TestReadBoundedAuthoritativeResultRejectsPathIdentitySwap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "result")
	replacement := filepath.Join(dir, "replacement")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	raw, err := readBoundedAuthoritativeResultWithOpener(
		path,
		func(path string) (*os.File, error) {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
			if err := os.Rename(replacement, path); err != nil {
				return nil, err
			}
			return openAuthoritativeResultFile(path)
		},
	)
	if !errors.Is(err, ErrAuthoritativeResultUnsafe) {
		t.Fatalf("identity-swapped read error = %v, want ErrAuthoritativeResultUnsafe", err)
	}
	if raw != nil {
		t.Fatalf("identity-swapped read returned %q, want no bytes", raw)
	}
}

func TestReadBoundedAuthoritativeResultOpenFailureIsStatic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	const secretPath = "AUTHORITATIVE-RESULT-PATH-SECRET"
	raw, err := readBoundedAuthoritativeResultWithOpener(
		path,
		func(string) (*os.File, error) {
			return nil, &os.PathError{
				Op:   "open",
				Path: secretPath,
				Err:  os.ErrPermission,
			}
		},
	)
	if err != ErrAuthoritativeResultUnsafe {
		t.Fatalf("open failure = %v, want exact ErrAuthoritativeResultUnsafe", err)
	}
	if strings.Contains(err.Error(), secretPath) {
		t.Fatalf("open failure surfaced path: %v", err)
	}
	if raw != nil {
		t.Fatalf("open failure returned %q, want no bytes", raw)
	}
}

func TestReadBoundedAuthoritativeResultRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "result")
	if err := os.WriteFile(target, []byte("SYMLINK-TARGET-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	raw, err := readBoundedAuthoritativeResult(link)
	if !errors.Is(err, ErrAuthoritativeResultUnsafe) {
		t.Fatalf("symlink read error = %v, want ErrAuthoritativeResultUnsafe", err)
	}
	if raw != nil {
		t.Fatalf("symlink read returned %q, want no bytes", raw)
	}
}
