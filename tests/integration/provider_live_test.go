package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

type liveWorkerResult struct {
	Outcome  string   `json:"outcome"`
	Summary  string   `json:"summary"`
	Evidence []string `json:"evidence"`
}

func TestLiveClaudeRunnerTurn(t *testing.T) {
	requireLiveClaudeRunner(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := provider.ClaudeRunner{}.Run(ctx, provider.Binding{
		Name:   "claude",
		Config: config.DefaultClaudeProvider(),
	}, provider.Request{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "Return exactly one compact JSON object and no surrounding prose.",
		UserPrompt:   `Reply with {"outcome":"done","summary":"live claude smoke","evidence":["live-claude"]} and nothing else.`,
		Model:        variant.ModelHaiku,
		Effort:       "medium",
	})
	if err != nil {
		t.Fatalf("ClaudeRunner.Run: %v", err)
	}

	var parsed liveWorkerResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.AssistantOutput)), &parsed); err != nil {
		t.Fatalf("claude output was not valid JSON: %v\n%s", err, result.AssistantOutput)
	}
	if parsed.Outcome != "done" {
		t.Fatalf("unexpected claude outcome: %+v", parsed)
	}
	if len(parsed.Evidence) == 0 || parsed.Evidence[0] != "live-claude" {
		t.Fatalf("unexpected claude evidence: %+v", parsed)
	}
}

func TestLiveCodexRunnerTurn(t *testing.T) {
	requireLiveCodex(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := provider.CodexRunner{}.Run(ctx, provider.Binding{
		Name:   "codex",
		Config: config.DefaultCodexProvider(),
	}, provider.Request{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "Return only a final JSON object matching the provided schema.",
		UserPrompt:   `Reply with outcome "done", a short summary proving you ran, and one evidence string "live-codex".`,
		OutputSchema: `{"type":"object","required":["outcome","summary","evidence"],"properties":{"outcome":{"type":"string","enum":["done"]},"summary":{"type":"string","minLength":1},"evidence":{"type":"array","items":{"type":"string"},"minItems":1}},"additionalProperties":false}`,
		Model:        variant.ModelSonnet,
		Effort:       "medium",
	})
	if err != nil {
		t.Fatalf("CodexRunner.Run: %v", err)
	}

	var parsed liveWorkerResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.AssistantOutput)), &parsed); err != nil {
		t.Fatalf("codex output was not valid JSON: %v\n%s", err, result.AssistantOutput)
	}
	if parsed.Outcome != "done" {
		t.Fatalf("unexpected codex outcome: %+v", parsed)
	}
	if len(parsed.Evidence) == 0 || parsed.Evidence[0] != "live-codex" {
		t.Fatalf("unexpected codex evidence: %+v", parsed)
	}
}

func TestLiveGeminiRunnerTurn(t *testing.T) {
	requireLiveGemini(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := provider.GeminiRunner{}.Run(ctx, provider.Binding{
		Name:   "gemini",
		Config: config.DefaultGeminiProvider(),
	}, provider.Request{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "Return exactly one compact JSON object and no surrounding prose.",
		UserPrompt:   `Reply with {"outcome":"done","summary":"live gemini smoke","evidence":["live-gemini"]} and nothing else.`,
		Model:        variant.ModelSonnet,
		Effort:       "medium",
	})
	if err != nil {
		t.Fatalf("GeminiRunner.Run: %v", err)
	}

	var parsed liveWorkerResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.AssistantOutput)), &parsed); err != nil {
		t.Fatalf("gemini output was not valid JSON: %v\n%s", err, result.AssistantOutput)
	}
	if parsed.Outcome != "done" {
		t.Fatalf("unexpected gemini outcome: %+v", parsed)
	}
	if len(parsed.Evidence) == 0 || parsed.Evidence[0] != "live-gemini" {
		t.Fatalf("unexpected gemini evidence: %+v", parsed)
	}
}

func requireLiveCodex(t *testing.T) {
	t.Helper()
	if os.Getenv("CODEX_AUTHENTICATED") != "1" {
		t.Skip("CODEX_AUTHENTICATED != 1; skipping live codex runner smoke test")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex binary not on PATH")
	}
	cmd := exec.Command("codex", "login", "status")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("codex login status failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}
}

func requireLiveClaudeRunner(t *testing.T) {
	t.Helper()
	if os.Getenv("CLAUDE_AUTHENTICATED") != "1" {
		t.Skip("CLAUDE_AUTHENTICATED != 1; skipping live claude runner smoke test")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude binary not on PATH")
	}
}

func requireLiveGemini(t *testing.T) {
	t.Helper()
	if os.Getenv("GEMINI_AUTHENTICATED") != "1" {
		t.Skip("GEMINI_AUTHENTICATED != 1; skipping live gemini runner smoke test")
	}
	if _, err := exec.LookPath("gemini"); err != nil {
		t.Skip("gemini binary not on PATH")
	}
	if os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
		t.Skip("Gemini live smoke requires GEMINI_API_KEY or GOOGLE_API_KEY")
	}
}
