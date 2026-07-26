package provider

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCodexRunnerPinsExactModelAndEffort(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	argsPath := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("RALPH_TEST_ARGS_PATH", argsPath)
	bin := writeFakeCLI(t, "fake-codex-exact.sh", `#!/bin/sh
printf '%s\n' "$@" > "$RALPH_TEST_ARGS_PATH"
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message) out="$2"; shift 2 ;;
    *) shift 1 ;;
  esac
done
printf '%s' '{"outcome":"done","summary":"exact","evidence":["exact"]}' > "$out"
`)
	binding := Binding{
		Name:   "codex-sol-xhigh",
		Config: BindingConfig{Type: "codex", Binary: bin},
	}
	result, err := CodexRunner{}.Run(context.Background(), binding, Request{
		WorkingDir: t.TempDir(), UserPrompt: "test",
		Model: Model("gpt-5.6-sol"), Effort: "xhigh", StrictBinding: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	joined := strings.Join(args, "\x00")
	if !strings.Contains(joined, "-m\x00gpt-5.6-sol") ||
		!strings.Contains(joined, "-c\x00model_reasoning_effort=\"xhigh\"") {
		t.Fatalf("codex args did not pin exact invocation: %q", args)
	}
	if result.Invocation != (Invocation{
		Alias: "codex-sol-xhigh", Provider: "codex",
		Model: "gpt-5.6-sol", Effort: "xhigh",
	}) {
		t.Fatalf("invocation provenance = %+v", result.Invocation)
	}
}

func TestResolveInvocationTreatsCalibratedDefaultAsExplicitNoFlag(t *testing.T) {
	invocation, err := ResolveInvocation(Binding{
		Name:   "opencode-qwen-default",
		Config: BindingConfig{Type: "opencode", Binary: "opencode"},
	}, Request{
		Model: Model("ollama/qwen3.5:4b"), Effort: "default", StrictBinding: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Model != "ollama/qwen3.5:4b" || invocation.Effort != "" {
		t.Fatalf("invocation = %+v", invocation)
	}
}

func TestResolveInvocationRejectsAliasThatMasksWrongProviderModel(t *testing.T) {
	binding, err := ResolveShippedBinding("codex")
	if err != nil {
		t.Fatal(err)
	}
	binding.Name = "claude-sonnet-medium"
	_, err = ResolveInvocation(binding, Request{
		Model: Model("claude-sonnet-4-6"), Effort: "medium", StrictBinding: true,
	})
	if err == nil || !strings.Contains(err.Error(), "not an exact codex model identifier") {
		t.Fatalf("ResolveInvocation error = %v, want provider/model mismatch", err)
	}
}
