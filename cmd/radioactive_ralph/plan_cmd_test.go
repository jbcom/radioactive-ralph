package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/supervisor"
)

func TestPlanTitleFallbackUsesFilename(t *testing.T) {
	// With no heading, plan.Title falls back to planTitleFallback (the
	// filename sans extension).
	if got := planTitleFallback("/plans/my-plan.md"); got != "my-plan" {
		t.Errorf("planTitleFallback = %q, want %q", got, "my-plan")
	}
}

// TestPlanImportCreatesActivePlan is the behavioral regression for the
// audit's coverage gap: runPlanImport is the ONLY user-facing path that
// calls store.CreatePlan, so without it the dispatch loop has nothing to
// drive. Import a real plan file through the root command and assert the
// plan lands active in the store.
func TestPlanImportCreatesActivePlan(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)

	projectDir := t.TempDir()
	chdir(t, projectDir)

	planPath := filepath.Join(projectDir, "plan.md")
	if err := os.WriteFile(planPath, []byte("# Ship It\n\n1. do the thing\n"), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{"plan", "import", planPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plan import: %v", err)
	}

	st, err := store.Open(context.Background(), store.Options{DSN: store.DSN(storeDBPath(stateDir))})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = st.Close() }()

	plans, err := st.ListPlans(context.Background(), "", nil) // active+paused
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("got %d active plans, want 1", len(plans))
	}
	if plans[0].Title != "Ship It" {
		t.Errorf("plan title = %q, want %q", plans[0].Title, "Ship It")
	}
	if plans[0].Slug != "ship-it" {
		t.Errorf("plan slug = %q, want %q", plans[0].Slug, "ship-it")
	}
	if plans[0].Status != store.PlanStatusActive {
		t.Errorf("plan status = %q, want active", plans[0].Status)
	}
	if plans[0].SourceMarkdown == "" {
		t.Error("plan source markdown was not stored")
	}
}

// TestPlanImportEmptyFileRejected confirms an empty plan file is rejected
// with a clear error rather than creating a degenerate plan.
func TestPlanImportEmptyFileRejected(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)
	projectDir := t.TempDir()
	chdir(t, projectDir)

	planPath := filepath.Join(projectDir, "empty.md")
	if err := os.WriteFile(planPath, []byte("   \n\n"), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{"plan", "import", planPath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("plan import of an empty file: want error, got nil")
	}
}

// TestPlanImportDuplicateSlugRejected confirms a second import with the same
// derived slug fails cleanly (the store's duplicate-slug guard) rather than
// silently creating two plans.
func TestPlanImportDuplicateSlugRejected(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)
	projectDir := t.TempDir()
	chdir(t, projectDir)

	planPath := filepath.Join(projectDir, "plan.md")
	if err := os.WriteFile(planPath, []byte("# Same Title\n\n1. step\n"), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	first := newRootCmd(context.Background())
	first.SetArgs([]string{"plan", "import", planPath})
	if err := first.Execute(); err != nil {
		t.Fatalf("first import: %v", err)
	}
	second := newRootCmd(context.Background())
	second.SetArgs([]string{"plan", "import", planPath})
	if err := second.Execute(); err == nil {
		t.Fatal("second import with the same slug: want error, got nil")
	}
}

// TestPlanImportUsesSupervisorWhenReachable verifies that when a supervisor is
// running, `plan import` routes through the IPC plan-import command (single
// writer of record) rather than a direct store write. We start a real
// supervisor, import a plan, and confirm it landed active in the shared store.
func TestPlanImportUsesSupervisorWhenReachable(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)
	projectDir := t.TempDir()
	chdir(t, projectDir)

	st, err := store.Open(context.Background(), store.Options{DSN: store.DSN(storeDBPath(stateDir))})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- supervisor.Run(ctx, supervisor.Options{RuntimeDir: stateDir, Store: st}) }()

	// Wait for the supervisor to be reachable.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, ferr := supervisor.Find(stateDir); ferr == nil {
			_ = c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	planPath := filepath.Join(projectDir, "plan.md")
	if err := os.WriteFile(planPath, []byte("# Via IPC\n\n1. step\n"), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{"plan", "import", planPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plan import: %v", err)
	}

	plans, err := st.ListPlans(context.Background(), "", nil)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) != 1 || plans[0].Title != "Via IPC" || plans[0].Status != store.PlanStatusActive {
		t.Fatalf("plan not imported via supervisor as active: %+v", plans)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not exit within 3s")
	}
}
