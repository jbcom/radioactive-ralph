package provider

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// fakeCodexCLI returns a shell script standing in for the `codex` binary: it
// scans its args for `--output-last-message <path>`, writes msg there, prints
// stdout, and exits with code. This lets the tests exercise CodexRunner's
// exit-code handling without a real codex install.
func fakeCodexCLI(t *testing.T, name, msg, stdout string, code int) string {
	t.Helper()
	return writeFakeCLI(t, name, `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message)
      out="$2"
      shift 2
      ;;
    *)
      shift 1
      ;;
  esac
done
[ -n "$out" ] && printf '%s' '`+msg+`' > "$out"
printf '%s\n' '`+stdout+`'
exit `+strconv.Itoa(code)+`
`)
}

// TestCodexRunnerFailsOnNonzeroExit is the regression guard for the
// laundered-failure bug: codex has no structured terminal frame, so a failed
// CLI run that wrote a partial message to --output-last-message before exiting
// nonzero must FAIL the turn, not be reported as a successful (and zero-cost)
// result.
func TestCodexRunnerFailsOnNonzeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := fakeCodexCLI(t, "fake-codex-fail.sh", "partial diagnostic output", "some stderr-ish line", 1)
	_, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true, // fake binary path is local.toml-authorized
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if err == nil {
		t.Fatal("codex exited nonzero after writing a partial message; want the turn to FAIL, got success")
	}
	if !strings.Contains(err.Error(), "exited nonzero") {
		t.Fatalf("err = %v, want it to mention a nonzero exit", err)
	}
}

// TestCodexRunnerSucceedsOnCleanExit confirms the exit gate does not regress the
// happy path: a codex that writes its message and exits 0 yields the message.
func TestCodexRunnerSucceedsOnCleanExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := fakeCodexCLI(t, "fake-codex-ok.sh", `{"outcome":"done","summary":"codex ok","evidence":["c"]}`, "done", 0)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:            "codex",
		BinaryFromLocal: true,
		Config:          BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("clean codex exit should succeed: %v", err)
	}
	if !strings.Contains(result.AssistantOutput, "codex ok") {
		t.Fatalf("AssistantOutput = %q, want the written message", result.AssistantOutput)
	}
}
