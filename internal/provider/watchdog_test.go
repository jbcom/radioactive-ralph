package provider

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agent"
)

// TestSuperviseAgentKillsOnPromptAndReturnsErrAgentBlocked is the proof the
// control invariant (spec §1: an agent CLI must NEVER block the system) is
// actually enforced: a fake CLI that emits a permission-prompt line and then
// sleeps "forever" must be killed by superviseAgent within its stall
// detection, not hang the caller.
func TestSuperviseAgentKillsOnPromptAndReturnsErrAgentBlocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-prompting-cli.sh", `#!/bin/sh
printf 'working...\n'
printf 'Do you want to proceed? (y/n)\n'
sleep 300
`)
	a, err := agent.Start(context.Background(), agent.Options{Command: bin})
	if err != nil {
		t.Fatalf("agent.Start: %v", err)
	}
	defer func() { _ = a.Kill() }()

	done := make(chan error, 1)
	go func() {
		done <- superviseAgent(context.Background(), a, agent.WatchdogConfig{
			StallTimeout:   2 * time.Second,
			PromptPatterns: DefaultPromptPatterns,
		}, nil)
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrAgentBlocked) {
			t.Fatalf("superviseAgent error = %v, want wrapping ErrAgentBlocked", err)
		}
		if !strings.Contains(err.Error(), "prompt") {
			t.Errorf("error %q should mention the prompt detection", err.Error())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("superviseAgent did not return promptly after a permission prompt — the agent was left to hang, violating the never-block control invariant")
	}

	// The proof that it was actually KILLED, not merely reported blocked:
	// the process must have exited (a.Done() closed) well before its
	// scripted 300s sleep would complete.
	select {
	case <-a.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("agent process was not killed after superviseAgent detected the prompt")
	}
}

// TestSuperviseAgentKillsOnStall proves the stall path (no prompt at all,
// just silence) is also enforced: the agent is killed and ErrAgentBlocked
// returned once StallTimeout elapses with no output.
func TestSuperviseAgentKillsOnStall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-silent-cli.sh", `#!/bin/sh
sleep 300
`)
	a, err := agent.Start(context.Background(), agent.Options{Command: bin})
	if err != nil {
		t.Fatalf("agent.Start: %v", err)
	}
	defer func() { _ = a.Kill() }()

	err = superviseAgent(context.Background(), a, agent.WatchdogConfig{
		StallTimeout: 500 * time.Millisecond,
	}, nil)
	if !errors.Is(err, ErrAgentBlocked) {
		t.Fatalf("superviseAgent error = %v, want wrapping ErrAgentBlocked", err)
	}

	select {
	case <-a.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("agent process was not killed after superviseAgent detected the stall")
	}
}

// TestSuperviseAgentOnLineStopsSupervision proves onLine returning true
// (the caller saw its terminal frame) makes superviseAgent kill the agent
// and return nil promptly, rather than waiting for a.Output() to close on
// its own — this is what lets ClaudeRunner/OpencodeRunner stop as soon as
// they've parsed the CLI's result frame even if the CLI process lingers.
func TestSuperviseAgentOnLineStopsSupervision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-lingering-cli.sh", `#!/bin/sh
printf 'result-line\n'
sleep 300
`)
	a, err := agent.Start(context.Background(), agent.Options{Command: bin})
	if err != nil {
		t.Fatalf("agent.Start: %v", err)
	}
	defer func() { _ = a.Kill() }()

	var sawLine bool
	done := make(chan error, 1)
	go func() {
		done <- superviseAgent(context.Background(), a, agent.WatchdogConfig{
			StallTimeout: time.Minute,
		}, func(line []byte) bool {
			sawLine = strings.Contains(string(line), "result-line")
			return true
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("superviseAgent = %v, want nil once onLine signals done", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("superviseAgent did not stop after onLine returned true")
	}
	if !sawLine {
		t.Error("onLine never observed the fake CLI's output line")
	}
}

// TestClaudeRunnerKilledByWatchdogOnPrompt is the end-to-end proof at the
// runner level: ClaudeRunner.Run, driving a fake `claude` CLI that emits a
// permission prompt and then sleeps, must return promptly with an
// ErrAgentBlocked-wrapped error rather than hang for the sleep's duration.
// TestClaudeRunnerKilledByWatchdogOnRawPrompt proves that a GENUINE raw
// interactive prompt (a non-JSON line) is still detected and kills the turn,
// even under StreamJSONWatchdogConfig. That config sets
// SkipPromptMatchOnJSONLines, which suppresses matching ONLY on valid JSON
// frames — the raw "Allow this action? (y/n)" line here is not JSON, so it
// still matches and trips the watchdog well before the fake's 300s sleep.
func TestClaudeRunnerKilledByWatchdogOnRawPrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-claude-prompting.sh", `#!/bin/sh
printf '%s\n' '{"type":"system","session_id":"sid"}'
IFS= read -r _
printf 'Allow this action? (y/n)\n'
sleep 300
`)

	origStall := DefaultStallTimeout
	DefaultStallTimeout = 2 * time.Second
	defer func() { DefaultStallTimeout = origStall }()

	start := time.Now()
	_, err := ClaudeRunner{}.Run(context.Background(), Binding{
		Name:   "claude",
		Config: BindingConfig{Type: "claude", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
	elapsed := time.Since(start)

	if !errors.Is(err, ErrAgentBlocked) {
		t.Fatalf("ClaudeRunner.Run error = %v, want wrapping ErrAgentBlocked", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("ClaudeRunner.Run took %s, want well under the fake CLI's 300s sleep — the watchdog must kill it, not wait it out", elapsed)
	}
}

// TestClaudeRunnerNotKilledByPromptWordsInJSONOutput is the third-pass
// regression: a claude assistant frame whose TEXT contains prompt-like words
// ("permission", "do you want to continue?") must NOT be misread as an
// interactive prompt and killed — it's ordinary model output. The fake emits
// such a frame then a clean result frame and exits; Run must succeed.
func TestClaudeRunnerNotKilledByPromptWordsInJSONOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-claude-benign-prompt-words.sh", `#!/bin/sh
printf '%s\n' '{"type":"system","session_id":"sid"}'
IFS= read -r _
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"You need permission to write there; do you want to continue?"}]}}'
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"done"}'
`)

	start := time.Now()
	res, err := ClaudeRunner{}.Run(context.Background(), Binding{
		Name:   "claude",
		Config: BindingConfig{Type: "claude", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
	if err != nil {
		t.Fatalf("ClaudeRunner.Run error = %v, want success — prompt-like words in a JSON frame must NOT trigger a false kill", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("Run took too long; the turn should complete on the result frame")
	}
	if !strings.Contains(res.AssistantOutput, "do you want to continue") {
		t.Errorf("assistant output = %q, want the benign frame text captured", res.AssistantOutput)
	}
}

// TestCodexRunnerKilledByWatchdogOnPrompt proves codex.go's rework onto
// internal/agent + superviseAgent (task 2 of the Phase 6c tech-debt pass)
// actually enforces the same control invariant claude/opencode get: a
// codex process that unexpectedly prints an interactive-looking prompt and
// then hangs is killed, not waited out.
func TestCodexRunnerKilledByWatchdogOnPrompt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake CLI is Unix-only")
	}
	bin := writeFakeCLI(t, "fake-codex-prompting.sh", `#!/bin/sh
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
printf 'thinking...\n'
printf 'Do you want to proceed? (y/n)\n'
sleep 300
`)

	origStall := DefaultStallTimeout
	DefaultStallTimeout = 2 * time.Second
	defer func() { DefaultStallTimeout = origStall }()

	start := time.Now()
	_, err := CodexRunner{}.Run(context.Background(), Binding{
		Name:   "codex",
		Config: BindingConfig{Type: "codex", Binary: bin},
	}, Request{WorkingDir: t.TempDir(), UserPrompt: "hi"})
	elapsed := time.Since(start)

	if !errors.Is(err, ErrAgentBlocked) {
		t.Fatalf("CodexRunner.Run error = %v, want wrapping ErrAgentBlocked", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("CodexRunner.Run took %s, want well under the fake CLI's 300s sleep — the watchdog must kill it, not wait it out", elapsed)
	}
}
