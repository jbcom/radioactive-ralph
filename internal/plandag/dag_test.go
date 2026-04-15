package plandag

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// TestDAGRoundTrip walks the full plandag lifecycle: create plan →
// seed tasks + deps → query ready set → claim atomically → mark done →
// verify downstream tasks become ready.
func TestDAGRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	planID, err := s.CreatePlan(ctx, CreatePlanOpts{
		Slug: "roundtrip", Title: "Roundtrip test",
		RepoPath: "/tmp/repo",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Topology:  a ──▶ c
	//                 ▲
	//            b ───┘
	// a and b are initially ready. c becomes ready only after both
	// predecessors are done.
	for _, id := range []string{"a", "b", "c"} {
		if err := s.CreateTask(ctx, CreateTaskOpts{
			PlanID: planID, ID: id, Description: "task " + id,
		}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}
	if err := s.AddDep(ctx, planID, "c", "a"); err != nil {
		t.Fatalf("AddDep c→a: %v", err)
	}
	if err := s.AddDep(ctx, planID, "c", "b"); err != nil {
		t.Fatalf("AddDep c→b: %v", err)
	}

	// Create a session + two variant rows so claims have valid FKs.
	sessID, err := s.CreateSession(ctx, SessionOpts{
		Mode: SessionModePortable, Transport: SessionTransportStdio,
		PID: 1, PIDStartTime: "2026-04-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sv1, err := s.CreateSessionVariant(ctx, SessionVariantOpts{
		SessionID: sessID, VariantName: "green",
		SubprocessPID: 1001, SubprocessStartTime: "2026-04-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateSessionVariant: %v", err)
	}
	sv2, err := s.CreateSessionVariant(ctx, SessionVariantOpts{
		SessionID: sessID, VariantName: "green",
		SubprocessPID: 1002, SubprocessStartTime: "2026-04-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateSessionVariant sv2: %v", err)
	}

	// Initial ready set should contain a and b, not c.
	ready, err := s.Ready(ctx, planID)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(ready) != 2 {
		t.Fatalf("initial ready = %d tasks, want 2 (a, b)", len(ready))
	}
	readyIDs := map[string]bool{ready[0].ID: true, ready[1].ID: true}
	if !readyIDs["a"] || !readyIDs["b"] {
		t.Errorf("ready set = %v, want {a, b}", readyIDs)
	}

	// Claim twice in a row. Atomic claim guarantees they're distinct.
	first, err := s.ClaimNextReady(ctx, planID, "green", sessID, sv1)
	if err != nil {
		t.Fatalf("Claim 1: %v", err)
	}
	second, err := s.ClaimNextReady(ctx, planID, "green", sessID, sv2)
	if err != nil {
		t.Fatalf("Claim 2: %v", err)
	}
	if first.ID == second.ID {
		t.Errorf("claim duplicated task %s; claims must be unique", first.ID)
	}

	// c should not be claimable yet.
	if _, err := s.ClaimNextReady(ctx, planID, "green", sessID, sv1); !errors.Is(err, ErrNoReadyTask) {
		t.Errorf("expected ErrNoReadyTask when all ready tasks claimed, got %v", err)
	}

	// Mark both predecessors done; c should enter the ready set.
	if _, err := s.MarkDone(ctx, planID, "a", sessID, `{}`); err != nil {
		t.Fatalf("MarkDone a: %v", err)
	}
	newReady, err := s.MarkDone(ctx, planID, "b", sessID, `{}`)
	if err != nil {
		t.Fatalf("MarkDone b: %v", err)
	}
	if len(newReady) != 1 || newReady[0].ID != "c" {
		t.Errorf("after completing a+b, ready set = %+v, want only c", newReady)
	}
}

// TestCycleRejection confirms AddDep rejects edges that would
// introduce a cycle.
func TestCycleRejection(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	planID, err := s.CreatePlan(ctx, CreatePlanOpts{Slug: "cyc", Title: "cycle test"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	for _, id := range []string{"x", "y", "z"} {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: id, Description: id}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}

	// Chain x → y → z; adding z → x must be rejected.
	if err := s.AddDep(ctx, planID, "y", "x"); err != nil {
		t.Fatalf("x→y setup: %v", err)
	}
	if err := s.AddDep(ctx, planID, "z", "y"); err != nil {
		t.Fatalf("y→z setup: %v", err)
	}
	if err := s.AddDep(ctx, planID, "x", "z"); err == nil {
		t.Error("expected cycle rejection on x→z, got nil")
	}

	// Self-dep must also fail.
	if err := s.AddDep(ctx, planID, "x", "x"); err == nil {
		t.Error("expected self-dep rejection, got nil")
	}
}

// TestRetryRequeues confirms MarkFailed requeues up to maxRetries
// and transitions to failed afterward.
func TestRetryRequeues(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	planID, err := s.CreatePlan(ctx, CreatePlanOpts{Slug: "retry", Title: "retry test"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := s.CreateTask(ctx, CreateTaskOpts{
		PlanID: planID, ID: "flaky", Description: "flaky task",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Set up the FK targets for claim().
	sessID, err := s.CreateSession(ctx, SessionOpts{
		Mode: SessionModePortable, Transport: SessionTransportStdio,
		PID: 1, PIDStartTime: "2026-04-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	svID, err := s.CreateSessionVariant(ctx, SessionVariantOpts{
		SessionID: sessID, VariantName: "green",
		SubprocessPID: 9001, SubprocessStartTime: "2026-04-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateSessionVariant: %v", err)
	}

	// Claim + fail + fail + fail → maxRetries=2 means third fail sticks.
	for attempt := 1; attempt <= 3; attempt++ {
		claimed, err := s.ClaimNextReady(ctx, planID, "green", sessID, svID)
		if err != nil {
			t.Fatalf("attempt %d claim: %v", attempt, err)
		}
		if claimed.ID != "flaky" {
			t.Fatalf("attempt %d claimed wrong task: %s", attempt, claimed.ID)
		}
		retried, err := s.MarkFailed(ctx, planID, "flaky", sessID, "boom", 2)
		if err != nil {
			t.Fatalf("attempt %d MarkFailed: %v", attempt, err)
		}
		wantRetried := attempt <= 2
		if retried != wantRetried {
			t.Errorf("attempt %d: retried=%v, want %v", attempt, retried, wantRetried)
		}
	}

	// After three fails, the task is 'failed' — not claimable.
	task, err := s.GetTask(ctx, planID, "flaky")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != TaskStatusFailed {
		t.Errorf("final status = %q, want %q", task.Status, TaskStatusFailed)
	}
}
