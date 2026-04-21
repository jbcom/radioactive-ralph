package provider

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

func TestNewRunnerSupportsBuiltins(t *testing.T) {
	cases := []struct {
		name string
		typ  string
	}{
		{name: "claude", typ: "claude"},
		{name: "codex", typ: "codex"},
		{name: "gemini", typ: "gemini"},
		{name: "plain", typ: "plain-stdout"},
		{name: "file", typ: "last-message-file"},
		{name: "stream", typ: "stream-json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner, err := NewRunner(Binding{Config: config.ProviderFile{Type: tc.typ}})
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
		Name: "plain",
		Config: config.ProviderFile{
			Type:   "plain-stdout",
			Binary: bin,
			Args:   []string{"--model={model}", "{prompt}"},
		},
	}, Request{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "system",
		UserPrompt:   `{"outcome":"done","summary":"plain ok","evidence":["plain"]}`,
		Model:        variant.ModelSonnet,
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
		Name: "file",
		Config: config.ProviderFile{
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
		Name: "stream",
		Config: config.ProviderFile{
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

func TestValidateBindingRejectsUnknownTemplateToken(t *testing.T) {
	err := ValidateBinding(Binding{
		Name: "bad",
		Config: config.ProviderFile{
			Type:   "plain-stdout",
			Binary: "sh",
			Args:   []string{"{modl}"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown template token") {
		t.Fatalf("ValidateBinding error = %v, want unknown template token", err)
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
		Config: config.ProviderFile{Type: "codex", Binary: bin},
	}, Request{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "system",
		UserPrompt:   "user",
		OutputSchema: `{"type":"object"}`,
		Model:        variant.ModelSonnet,
		Effort:       "high",
	})
	if err != nil {
		t.Fatalf("CodexRunner.Run: %v", err)
	}
	if !strings.Contains(result.AssistantOutput, `"outcome":"done"`) {
		t.Fatalf("unexpected codex output: %q", result.AssistantOutput)
	}
}

func TestGeminiRunnerReturnsStdout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-gemini.sh", `#!/bin/sh
printf '%s' '{"outcome":"blocked","summary":"need more context","evidence":[],"reason":"missing release notes","needs_context":["release-notes"]}'
`)
	result, err := GeminiRunner{}.Run(context.Background(), Binding{
		Name:   "gemini",
		Config: config.ProviderFile{Type: "gemini", Binary: bin},
	}, Request{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "system",
		UserPrompt:   "user",
		Model:        variant.ModelSonnet,
		Effort:       "medium",
	})
	if err != nil {
		t.Fatalf("GeminiRunner.Run: %v", err)
	}
	if !strings.Contains(result.AssistantOutput, `"blocked"`) {
		t.Fatalf("unexpected gemini output: %q", result.AssistantOutput)
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
		Config: config.ProviderFile{Type: "claude", Binary: bin},
	}, Request{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "system",
		UserPrompt:   "user",
		Model:        variant.ModelSonnet,
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
