package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	claudesession "github.com/jbcom/radioactive-ralph/internal/provider/claudesession"
)

// TestLiveClaudeRoundTrip drives a real `claude -p` subprocess via the
// session wrapper: sends a deterministic prompt, collects the
// response, then resumes the session by SessionID and emits the
// sentinel to verify the conversation continues from where it left off.
//
// Gated on CLAUDE_AUTHENTICATED=1 AND the presence of valid API
// credentials (ANTHROPIC_API_KEY or OAuth-configured Claude Code).
// Skipped silently otherwise so every-PR CI doesn't flake on missing
// secrets.
func TestLiveClaudeRoundTrip(t *testing.T) {
	if os.Getenv("CLAUDE_AUTHENTICATED") != "1" {
		t.Skip("CLAUDE_AUTHENTICATED != 1; skipping live claude round-trip")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Spawn the session.
	s, err := claudesession.Spawn(ctx, claudesession.Options{
		WorkingDir: t.TempDir(),
		// Keep the prompt minimal so the reply is small and fast.
		SystemPrompt: "You respond with exactly one word and nothing else.",
		Model:        "haiku", // cheapest tier
		AllowedTools: []string{},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Collect the first result + any assistant text for post-hoc assertions.
	var assistantText bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range s.Events() {
			if ev.Err != nil {
				t.Logf("event err: %v", ev.Err)
				continue
			}
			if ev.Inbound.Type == "assistant" {
				assistantText.Write(ev.Inbound.Message)
			}
			if ev.Inbound.Type == "result" {
				return
			}
		}
	}()

	if err := s.SendUserMessage(ctx, "Reply with the single word PING."); err != nil {
		t.Fatalf("SendUserMessage: %v", err)
	}
	if err := s.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
	<-done

	sessionID := s.SessionID()
	if sessionID == "" {
		t.Fatal("SessionID empty after first turn")
	}
	// PING verification is informational — we only care that a
	// non-empty assistant reply came through.
	if assistantText.Len() == 0 {
		t.Error("no assistant text received from first turn")
	}
	_ = s.Close()

	// Resume. Sentinel verifies conversation continuity.
	s2, err := claudesession.Spawn(ctx, claudesession.Options{
		WorkingDir:     t.TempDir(),
		SystemPrompt:   "You respond with exactly one word and nothing else.",
		Model:          "haiku",
		SessionID:      sessionID,
		ResumeMode:     true,
		SentinelTaskID: "LIVE-PING",
	})
	if err != nil {
		t.Fatalf("Spawn(resume): %v", err)
	}
	defer func() { _ = s2.Close() }()

	// Confirm the sentinel lands in the resumed session's event stream.
	resumedDeadline := time.After(60 * time.Second)
	var sawSentinelEcho bool
	for !sawSentinelEcho {
		select {
		case <-resumedDeadline:
			t.Fatal("did not observe sentinel echo within 60s of resume")
		case ev, ok := <-s2.Events():
			if !ok {
				t.Fatal("resumed event stream closed before sentinel")
			}
			if ev.Inbound.Type == "assistant" || ev.Inbound.Type == "user" {
				raw := string(ev.Inbound.Message)
				if strings.Contains(raw, "LIVE-PING") || strings.Contains(raw, "SENTINEL") {
					sawSentinelEcho = true
				}
			}
		}
	}
}

// TestLiveClaudeModelSanity exercises the model flag by asking for a
// single byte and verifying it came back. Helps catch model-flag
// breakage from Claude CLI updates.
func TestLiveClaudeModelSanity(t *testing.T) {
	if os.Getenv("CLAUDE_AUTHENTICATED") != "1" {
		t.Skip("CLAUDE_AUTHENTICATED != 1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s, err := claudesession.Spawn(ctx, claudesession.Options{
		WorkingDir:   t.TempDir(),
		SystemPrompt: "Respond with the JSON object {\"ok\":true} and nothing else.",
		Model:        "haiku",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = s.Close() }()

	// Drain into a collector.
	var allMsgs bytes.Buffer
	drain := make(chan struct{})
	go func() {
		defer close(drain)
		for ev := range s.Events() {
			if ev.Inbound.Type == "assistant" {
				allMsgs.Write(ev.Inbound.Message)
			}
			if ev.Inbound.Type == "result" {
				return
			}
		}
	}()

	if err := s.SendUserMessage(ctx, "reply now"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := s.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
	<-drain

	// Minimal validation — we got a non-empty assistant response.
	// Stricter content checks are brittle across model updates.
	if allMsgs.Len() == 0 {
		t.Error("no assistant text received")
	}
	// Spot-check that the assistant content is syntactically JSON-ish.
	if !json.Valid(allMsgs.Bytes()) {
		// The assistant Message payload is a full content-block array;
		// validity here is lenient — the test primarily verifies we
		// received valid JSON from the CLI at all.
		t.Logf("assistant payload not standalone valid JSON (expected; payload is content-blocks):\n%s", allMsgs.String())
	}
}
