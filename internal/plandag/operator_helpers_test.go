package plandag

import (
	"context"
	"path/filepath"
	"testing"
)

// seedPlanTasks creates a plan with the named tasks (no deps), plus a
// session+variant row. Returns the plan id, session id, and variant id.
// Operator-method tests that need to put a specific task into
// running/blocked/etc. state should claim the task via the returned
// session/variant first.
func seedPlanTasks(t *testing.T, s *Store, slug string, tasks ...string) (planID, sessID, svID string) {
	t.Helper()
	ctx := context.Background()
	planID, err := s.CreatePlan(ctx, CreatePlanOpts{Slug: slug, Title: slug, RepoPath: "/tmp/repo"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	for _, id := range tasks {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: id, Description: id}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}
	sessID, err = s.CreateSession(ctx, SessionOpts{
		Mode: SessionModeDurable, Transport: SessionTransportSocket,
		PID: 7, PIDStartTime: "service-7", Host: "local",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	svID, err = s.CreateSessionVariant(ctx, SessionVariantOpts{
		SessionID: sessID, VariantName: "green",
		SubprocessPID: 7700, SubprocessStartTime: "2026-04-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateSessionVariant: %v", err)
	}
	return planID, sessID, svID
}

// seedPlanWithDeps creates a plan with parent→child dep plus an
// isolated "solo" task, plus a session+variant row. Used by tests
// that exercise the dependency-unblocking semantics.
func seedPlanWithDeps(t *testing.T, s *Store, slug string) (planID, sessID, svID string) {
	t.Helper()
	planID, sessID, svID = seedPlanTasks(t, s, slug, "parent", "child", "solo")
	if err := s.AddDep(context.Background(), planID, "child", "parent"); err != nil {
		t.Fatalf("AddDep child→parent: %v", err)
	}
	return
}

// claimTask claims the named task via a fresh session+variant, so a
// test can put a specific task into running state without disturbing
// the shared seed session. Returns the session id used for the claim
// (so the test can MarkDone/MarkBlocked against it).
func claimTask(t *testing.T, s *Store, planID, taskID, variant string) (sessID string) {
	t.Helper()
	ctx := context.Background()
	sessID, err := s.CreateSession(ctx, SessionOpts{
		Mode: SessionModeDurable, Transport: SessionTransportSocket,
		PID: 100, PIDStartTime: "service-100", Host: "local",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	svID, err := s.CreateSessionVariant(ctx, SessionVariantOpts{
		SessionID: sessID, VariantName: variant,
		SubprocessPID: 10100, SubprocessStartTime: "2026-04-15T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateSessionVariant: %v", err)
	}
	claimed, err := s.ClaimNextReady(ctx, planID, variant, sessID, svID)
	if err != nil {
		t.Fatalf("ClaimNextReady %s: %v", taskID, err)
	}
	if claimed.ID != taskID {
		t.Fatalf("claimed %s, want %s", claimed.ID, taskID)
	}
	return sessID
}

func containsTask(tasks []Task, id string) bool {
	for _, t := range tasks {
		if t.ID == id {
			return true
		}
	}
	return false
}

// freshStore opens a plandag Store backed by a per-test tempdir SQLite
// file. Centralizes the Open boilerplate so each operator test file
// stays short.
func freshStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), Options{DSN: "file:" + filepath.Join(t.TempDir(), "plans.db")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
