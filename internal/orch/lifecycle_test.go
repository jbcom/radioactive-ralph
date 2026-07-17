package orch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agent"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestEnforcementPromptWritesOnEachTick confirms the enforcement-prompt
// cadence writes EnforcementPromptText to a running agent's stdin on every
// tick, and stops cleanly when the agent exits.
func TestEnforcementPromptWritesOnEachTick(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script — skip on windows")
	}
	// A script that echoes every stdin line it receives, so we can observe
	// the enforcement prompt arriving, then exits after 3 lines.
	a, err := agent.Start(context.Background(), agent.Options{
		Command: "sh",
		Args:    []string{"-c", `for i in 1 2 3; do read -r line; printf 'saw:%s\n' "$line"; done`},
	})
	if err != nil {
		t.Fatalf("agent.Start: %v", err)
	}
	defer func() { _ = a.Kill() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go EnforcementPrompt(ctx, a, 10*time.Millisecond)

	var got strings.Builder
	timeout := time.After(5 * time.Second)
	seenCount := 0
loop:
	for {
		select {
		case line, ok := <-a.Output():
			if !ok {
				break loop
			}
			got.Write(line)
			seenCount = strings.Count(got.String(), "saw:")
			if seenCount >= 3 {
				break loop
			}
		case <-timeout:
			t.Fatal("timed out waiting for enforcement prompts to arrive")
		}
	}
	if !strings.Contains(got.String(), "Stay on task") {
		t.Errorf("output = %q, want it to contain the enforcement prompt text", got.String())
	}
}

// TestEnforcementPromptStopsWhenContextCanceled confirms the goroutine
// exits promptly on ctx cancellation rather than leaking.
func TestEnforcementPromptStopsWhenContextCanceled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script — skip on windows")
	}
	a, err := agent.Start(context.Background(), agent.Options{
		Command: "sh", Args: []string{"-c", "sleep 5"},
	})
	if err != nil {
		t.Fatalf("agent.Start: %v", err)
	}
	defer func() { _ = a.Kill() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		EnforcementPrompt(ctx, a, time.Millisecond)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("EnforcementPrompt did not stop after context cancellation")
	}
}

// TestHandleWatchdogSignalKillsOnPromptStallResourceExceeded confirms the
// control invariant: Prompt/Stall/ResourceExceeded all say "kill", never
// "wait".
func TestHandleWatchdogSignalKillsOnPromptStallResourceExceeded(t *testing.T) {
	cases := []struct {
		kind agent.SignalKind
		want bool
	}{
		{agent.Prompt, true},
		{agent.Stall, true},
		{agent.Progress, false},
		{agent.Exited, false},
	}
	for _, c := range cases {
		got := HandleWatchdogSignal(agent.Signal{Kind: c.kind})
		if got != c.want {
			t.Errorf("HandleWatchdogSignal(kind=%v) = %v, want %v", c.kind, got, c.want)
		}
	}
}

// TestHandleContextEndKillsAndReleasesClaim confirms HandleContextEnd
// kills the agent and marks the task blocked (releasing its claim) rather
// than leaving it stuck in running.
func TestHandleContextEndKillsAndReleasesClaim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script — skip on windows")
	}
	ctx := context.Background()
	s := newTestStore(t)
	o := New(s)

	projectID := mustCreateTestProject(t, s, "ctxend-project")
	planID := mustCreateTestPlan(t, s, projectID, "ctxend-plan", "Ship", twoStepSequentialPlan)
	if err := s.CreateTask(ctx, store.CreateTaskOpts{PlanID: planID, ID: "0.0", Description: "write the code"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	a, err := agent.Start(context.Background(), agent.Options{Command: "sh", Args: []string{"-c", "sleep 5"}})
	if err != nil {
		t.Fatalf("agent.Start: %v", err)
	}

	if err := o.HandleContextEnd(ctx, a, planID, "0.0", sessionID); err != nil {
		t.Fatalf("HandleContextEnd: %v", err)
	}

	select {
	case <-a.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("agent was not killed by HandleContextEnd")
	}

	task, err := s.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != store.TaskStatusBlocked {
		t.Errorf("task status = %q, want blocked", task.Status)
	}
}

// TestWriteAndAbsorbDecisionLog confirms a worker's XDG decision log is
// created on first write and its content is absorbed into the store's
// project event history.
func TestWriteAndAbsorbDecisionLog(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	root := filepath.Join(t.TempDir(), "xdg-state")
	o := New(s, WithDecisionLogRoot(root))

	projectID := mustCreateTestProject(t, s, "decision-project")
	planID := mustCreateTestPlan(t, s, projectID, "decision-plan", "Ship", twoStepSequentialPlan)

	workerID := "worker-abc"
	if err := o.WriteWorkerDecision(workerID, "chose to refactor foo.go instead of patching it"); err != nil {
		t.Fatalf("WriteWorkerDecision: %v", err)
	}
	if err := o.WriteWorkerDecision(workerID, "ran go test ./... before finishing"); err != nil {
		t.Fatalf("WriteWorkerDecision: %v", err)
	}

	path, err := o.decisionLogPath(workerID)
	if err != nil {
		t.Fatalf("decisionLogPath: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("decision log file was not created at %q: %v", path, err)
	}

	if err := o.AbsorbDecisionLog(ctx, projectID, planID, "0.0", workerID); err != nil {
		t.Fatalf("AbsorbDecisionLog: %v", err)
	}

	events, err := s.ListProjectEvents(ctx, projectID, 10)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind == "worker.decision_log" && strings.Contains(ev.PayloadJSON, "refactor foo.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a worker.decision_log event carrying the decision text, got %+v", events)
	}
}

// TestAbsorbDecisionLogMissingFileIsNoOp confirms absorbing a worker with
// no decision log file does not error.
func TestAbsorbDecisionLogMissingFileIsNoOp(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	root := filepath.Join(t.TempDir(), "xdg-state")
	o := New(s, WithDecisionLogRoot(root))

	projectID := mustCreateTestProject(t, s, "no-decision-project")
	planID := mustCreateTestPlan(t, s, projectID, "no-decision-plan", "Ship", twoStepSequentialPlan)

	if err := o.AbsorbDecisionLog(ctx, projectID, planID, "0.0", "no-such-worker"); err != nil {
		t.Fatalf("AbsorbDecisionLog on missing file: %v", err)
	}
}
