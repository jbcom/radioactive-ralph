package provider

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func requirePython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skipf("python3 unavailable: %v", err)
	}
}

func TestCodexRunnerRejectsStructuredRecordBeyondInspectionBound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	requirePython3(t)
	bin := writeFakeCLI(t, "fake-codex-six-megabyte-jsonl.sh", `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then
    out="$arg"
  fi
  previous="$arg"
done
printf '%s' 'FORGED-SIX-MEGABYTE-SUCCESS' > "$out"
python3 -c 'import sys; sys.stdout.write("{\"type\":\"item.completed\",\"item\":{\"type\":\"command_execution\",\"aggregated_output\":\"" + "\\u0000" * (1 << 20) + "\"}}\n"); sys.stdout.flush()'
`)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if !errors.Is(err, ErrCodexOversizeSchema) {
		t.Fatalf("CodexRunner.Run error = %v, want ErrCodexOversizeSchema", err)
	}
	if result != (Result{}) {
		t.Fatalf("failed result = %+v, want no forged last-message result", result)
	}
	if strings.Contains(err.Error(), "FORGED-SIX-MEGABYTE-SUCCESS") {
		t.Fatalf("error surfaced authoritative-result payload: %v", err)
	}
}

func TestCodexRunnerAcceptsOfficial0145CommandEventWithinInspectionBound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	requirePython3(t)
	bin := writeFakeCLI(t, "fake-codex-official-command-event.sh", `#!/bin/sh
out=""
previous=""
for arg in "$@"; do
  if [ "$previous" = "--output-last-message" ]; then
    out="$arg"
  fi
  previous="$arg"
done
printf '%s' '{"outcome":"done","summary":"official event drained","evidence":[]}' > "$out"
python3 -c 'import json,sys; event={"type":"item.completed","item":{"id":"item-1","type":"command_execution","command":"c"*(70<<10),"aggregated_output":"\x00"*(512<<10),"exit_code":0,"status":"completed"}}; json.dump(event,sys.stdout,separators=(",",":")); sys.stdout.write("\n"); sys.stdout.flush()'
`)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("CodexRunner.Run rejected official 0.145 command event: %v", err)
	}
	if !strings.Contains(result.AssistantOutput, "official event drained") {
		t.Fatalf("AssistantOutput = %q, want authoritative last-message result", result.AssistantOutput)
	}
}

func TestCodexRunnerPreservesSmallFailureAfterLargeRetainedOfficialEvent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	requirePython3(t)
	bin := writeFakeCLI(t, "fake-codex-large-then-error.sh", `#!/bin/sh
python3 -c 'import json,sys; event={"type":"item.completed","item":{"id":"item-1","type":"command_execution","command":"c"*(70<<10),"aggregated_output":"\x00"*(512<<10),"exit_code":0,"status":"completed"}}; json.dump(event,sys.stdout,separators=(",",":")); sys.stdout.write("\n"); sys.stdout.write("{\"type\":\"error\",\"message\":\"quota exhausted AFTER-LARGE-SECRET\"}\n"); sys.stdout.flush()'
exit 19
`)
	_, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if err == nil {
		t.Fatal("CodexRunner.Run succeeded, want nonzero exit after retained error event")
	}
	if !strings.Contains(err.Error(), codexFailureQuota.String()) {
		t.Fatalf("error = %q, want retained quota category after large event", err)
	}
	if strings.Contains(err.Error(), "AFTER-LARGE-SECRET") {
		t.Fatalf("error surfaced provider content after large event: %q", err)
	}
}

func TestCodexRunnerEndlessNoNewlineOutputHonorsCallerCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	requirePython3(t)
	bin := writeFakeCLI(t, "fake-codex-endless-line.sh", `#!/bin/sh
python3 -u -c 'import sys
chunk=b"x"*(64<<10)
while True:
 sys.stdout.buffer.write(chunk)
 sys.stdout.buffer.flush()'
`)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := CodexRunner{}.Run(ctx, Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CodexRunner.Run error = %v, want wrapping context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("caller cancellation took %s to stop endless-progress Codex", elapsed)
	}
}
