package session

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/inventory"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// buildFakeClaude compiles the fake-claude test double into t.TempDir()
// and returns its absolute path. Cached via TestMain would be faster
// but this keeps the test independent.
func buildFakeClaude(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-claude")
	cmd := exec.Command("go", "build", "-o", bin,
		"github.com/jbcom/radioactive-ralph/internal/session/internal/fakeclaude")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fake-claude: %v\n%s", err, out)
	}
	return bin
}

func TestSpawnAndSendMessageRoundTrip(t *testing.T) {
	bin := buildFakeClaude(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := Spawn(ctx, Options{
		ClaudeBin:    bin,
		WorkingDir:   t.TempDir(),
		SystemPrompt: "test",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = s.Close() }()

	if s.SessionID() == "" {
		t.Error("SessionID should be populated post-Spawn")
	}

	if err := s.SendUserMessage(ctx, "hello"); err != nil {
		t.Fatalf("SendUserMessage: %v", err)
	}

	// Expect: system init, assistant echo, result.
	sawInit, sawAssistant, sawResult := false, false, false
	waitCh := time.After(5 * time.Second)
	for !sawInit || !sawAssistant || !sawResult {
		select {
		case <-waitCh:
			t.Fatalf("timeout; saw init=%v assistant=%v result=%v", sawInit, sawAssistant, sawResult)
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed before result")
			}
			if ev.Err != nil {
				t.Fatalf("event err: %v", ev.Err)
			}
			switch ev.Inbound.Type {
			case "system":
				sawInit = true
			case "assistant":
				sawAssistant = true
				// verify raw bytes roundtrip
				if !json.Valid(ev.Inbound.Raw) {
					t.Errorf("raw bytes not valid JSON: %s", ev.Inbound.Raw)
				}
				if len(ev.Inbound.Message) == 0 {
					t.Error("assistant frame missing message payload")
				}
			case "result":
				sawResult = true
			}
		}
	}
}

func TestWaitForIdleResolvesOnResult(t *testing.T) {
	bin := buildFakeClaude(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := Spawn(ctx, Options{ClaudeBin: bin, WorkingDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Drain events into the background.
	go func() {
		for range s.Events() { //nolint:revive // drain
		}
	}()

	if err := s.SendUserMessage(ctx, "do it"); err != nil {
		t.Fatalf("SendUserMessage: %v", err)
	}
	if err := s.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
}

func TestResumeRequiresSessionID(t *testing.T) {
	_, err := Spawn(context.Background(), Options{
		ClaudeBin:  "true", // unused — fails before exec
		ResumeMode: true,
	})
	if err == nil {
		t.Fatal("expected error when ResumeMode=true without SessionID")
	}
}

func TestResumeSendsSentinelOnSpawn(t *testing.T) {
	bin := buildFakeClaude(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Setenv("FAKE_CLAUDE_RESUME_ECHO", "1")
	s, err := Spawn(ctx, Options{
		ClaudeBin:      bin,
		WorkingDir:     t.TempDir(),
		ResumeMode:     true,
		SessionID:      "00000000-0000-0000-0000-000000000001",
		SentinelTaskID: "T42",
	})
	if err != nil {
		t.Fatalf("Spawn(resume): %v", err)
	}
	defer func() { _ = s.Close() }()

	// We expect to see the echo of our sentinel user message come back
	// as an assistant frame. Sentinel text is `SENTINEL: resuming task T42 …`.
	waitCh := time.After(5 * time.Second)
	for {
		select {
		case <-waitCh:
			t.Fatal("timeout waiting for sentinel echo")
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatal("events closed without echo")
			}
			if ev.Inbound.Type == "assistant" {
				raw := string(ev.Inbound.Message)
				if strings.Contains(raw, "T42") || strings.Contains(raw, "SENTINEL") {
					return
				}
			}
		}
	}
}

// ── PromptRenderer tests ---------------------------------------------

func TestRenderSystemPromptInjectsInventorySkill(t *testing.T) {
	p, _ := variant.Lookup("green")
	inv := inventory.Inventory{
		Skills: []inventory.Skill{{Name: "review", Plugin: "coderabbit"}},
	}
	out := RenderSystemPrompt(PromptOptions{
		Variant:   p,
		Inventory: inv,
	})
	if !strings.Contains(out, "coderabbit:review") {
		t.Errorf("expected coderabbit:review in prompt:\n%s", out)
	}
	if !strings.Contains(out, "green") {
		t.Errorf("expected variant name in prompt:\n%s", out)
	}
}

func TestRenderSystemPromptRespectsOperatorDisable(t *testing.T) {
	p, _ := variant.Lookup("green")
	inv := inventory.Inventory{
		Skills: []inventory.Skill{{Name: "review", Plugin: "coderabbit"}},
	}
	out := RenderSystemPrompt(PromptOptions{
		Variant:   p,
		Inventory: inv,
		OperatorChoices: map[variant.BiasCategory]BiasChoice{
			variant.BiasReview: {Disabled: true},
		},
	})
	if strings.Contains(out, "coderabbit:review") {
		t.Errorf("disabled bias should not appear:\n%s", out)
	}
}

func TestRenderSystemPromptOperatorChoiceOverridesInventoryHeuristic(t *testing.T) {
	p, _ := variant.Lookup("green")
	inv := inventory.Inventory{
		Skills: []inventory.Skill{
			{Name: "review", Plugin: "coderabbit"},
			{Name: "review", Plugin: "github"},
		},
	}
	out := RenderSystemPrompt(PromptOptions{
		Variant:   p,
		Inventory: inv,
		OperatorChoices: map[variant.BiasCategory]BiasChoice{
			variant.BiasReview: {Skill: "github:review"},
		},
	})
	if !strings.Contains(out, "github:review") {
		t.Errorf("operator choice github:review should win:\n%s", out)
	}
}

func TestRenderSystemPromptSkipsUnavailableSkill(t *testing.T) {
	p, _ := variant.Lookup("green")
	// Empty inventory — no review skill installed.
	out := RenderSystemPrompt(PromptOptions{Variant: p})
	// The preamble is always present, but no bias line should reference {skill}.
	if strings.Contains(out, "{skill}") {
		t.Errorf("unresolved {skill} placeholder leaked:\n%s", out)
	}
}

func TestRenderSystemPromptDeterministic(t *testing.T) {
	p, _ := variant.Lookup("green")
	inv := inventory.Inventory{
		Skills: []inventory.Skill{
			{Name: "review", Plugin: "a"},
			{Name: "review", Plugin: "b"},
			{Name: "docs_query", Plugin: "c"},
		},
	}
	a := RenderSystemPrompt(PromptOptions{Variant: p, Inventory: inv})
	b := RenderSystemPrompt(PromptOptions{Variant: p, Inventory: inv})
	if a != b {
		t.Errorf("prompt rendering not deterministic:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}
