package provider

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestOpencodeRunnerParsesStreamJSON(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	// Mirrors the real `opencode run --format json` event shape observed
	// from the installed opencode 1.18.3 CLI: a step_start frame, a text
	// frame carrying the assistant reply, then a step_finish frame with
	// token/cost usage.
	bin := writeFakeCLI(t, "fake-opencode.sh", `#!/bin/sh
printf '%s\n' '{"type":"step_start","sessionID":"ses_fake123","part":{"id":"prt_1","type":"step-start"}}'
printf '%s\n' '{"type":"text","sessionID":"ses_fake123","part":{"id":"prt_2","type":"text","text":"pong"}}'
printf '%s\n' '{"type":"step_finish","sessionID":"ses_fake123","part":{"id":"prt_3","type":"step-finish","tokens":{"total":110,"input":100,"output":10,"reasoning":0,"cache":{"write":0,"read":40}},"cost":0.0012}}'
`)
	result, err := OpencodeRunner{}.Run(context.Background(), Binding{
		Name:   "opencode",
		Config: BindingConfig{Type: "opencode", Binary: bin},
	}, Request{
		WorkingDir: t.TempDir(),
		UserPrompt: "reply with pong",
		Model:      ModelSonnet,
		Effort:     "high",
	})
	if err != nil {
		t.Fatalf("OpencodeRunner.Run: %v", err)
	}
	if result.SessionID != "ses_fake123" {
		t.Errorf("SessionID = %q, want ses_fake123", result.SessionID)
	}
	if !strings.Contains(result.AssistantOutput, "pong") {
		t.Fatalf("unexpected opencode output: %q", result.AssistantOutput)
	}
	if result.Usage.InputTokens != 100 || result.Usage.OutputTokens != 10 {
		t.Errorf("tokens = %d/%d, want 100/10", result.Usage.InputTokens, result.Usage.OutputTokens)
	}
	if result.Usage.CachedInputTokens != 40 {
		t.Errorf("CachedInputTokens = %d, want 40", result.Usage.CachedInputTokens)
	}
	if result.Usage.CostUSD != 0.0012 {
		t.Errorf("CostUSD = %v, want 0.0012", result.Usage.CostUSD)
	}
}

func TestOpencodeRunnerErrorsWithoutStepFinish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-opencode-noop.sh", `#!/bin/sh
printf '%s\n' '{"type":"step_start","sessionID":"ses_fake456","part":{"id":"prt_1","type":"step-start"}}'
`)
	_, err := OpencodeRunner{}.Run(context.Background(), Binding{
		Name:   "opencode",
		Config: BindingConfig{Type: "opencode", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
	if err == nil || !strings.Contains(err.Error(), "step_finish") {
		t.Fatalf("Run error = %v, want step_finish complaint", err)
	}
}

func TestParseOpencodeEventDiscardsNonJSONNoise(t *testing.T) {
	if _, ok := parseOpencodeEvent([]byte("")); ok {
		t.Error("empty line should not parse")
	}
	if _, ok := parseOpencodeEvent([]byte("not json at all")); ok {
		t.Error("non-JSON line should not parse")
	}
	if _, ok := parseOpencodeEvent([]byte(`{"sessionID":"x"}`)); ok {
		t.Error("frame without type should not parse")
	}
	ev, ok := parseOpencodeEvent([]byte(`{"type":"text","part":{"text":"hi"}}`))
	if !ok || ev.Part.Text != "hi" {
		t.Errorf("parseOpencodeEvent valid frame = %+v, ok=%v", ev, ok)
	}
}

func TestDefaultOpencodeProviderHasNativeFanout(t *testing.T) {
	cfg := defaultOpencodeProvider()
	if !cfg.NativeFanout {
		t.Error("opencode capability record should report NativeFanout=true (opencode agent create/list + run --agent)")
	}
	if cfg.Binary != "opencode" {
		t.Errorf("Binary = %q, want opencode", cfg.Binary)
	}
}

func TestDefaultClaudeProviderHasNativeFanout(t *testing.T) {
	cfg := defaultClaudeProvider()
	if !cfg.NativeFanout {
		t.Error("claude capability record should report NativeFanout=true (--agents/--agent/--forward-subagent-text)")
	}
}

func TestDefaultCodexProviderNativeFanoutUnconfirmed(t *testing.T) {
	cfg := defaultCodexProvider()
	if cfg.NativeFanout {
		t.Error("codex capability record should default NativeFanout=false — no verified subagent/parallel-workflow flag in codex exec --help")
	}
}

// TestDefaultAgyProviderDocumentsUnwiredCapability pins the shape of the
// agy capability record kept only for documentation purposes (see agy.go
// for the spike finding: agy --print requires a cloud project and is not
// local-only). It intentionally has no runner and is not reachable from
// NewRunner/builtInProvider/shippedProviderBinaries.
func TestDefaultAgyProviderDocumentsUnwiredCapability(t *testing.T) {
	cfg := defaultAgyProvider()
	if cfg.Type != "agy" || cfg.Binary != "agy" {
		t.Errorf("defaultAgyProvider() = %+v, want Type/Binary=agy", cfg)
	}
	if cfg.NativeFanout {
		t.Error("agy NativeFanout should be false — unverified, and agy is Unknown/unwired anyway")
	}
	if _, err := NewRunner(Binding{Config: cfg}); err == nil {
		t.Error("NewRunner should refuse an agy binding — no runner is registered for it")
	}
}
