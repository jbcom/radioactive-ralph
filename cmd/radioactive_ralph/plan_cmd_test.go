package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
// TestPlanImportRefusesWithoutSupervisor pins the single-writer contract.
//
// This test previously asserted that an offline import succeeded via a direct
// store write. That fallback is gone: AGENTS.md specifies the client "refuses
// to run without a supervisor", and a client that opens the DB itself is a
// second writer to a supervisor-owned database. It also silently produced a
// plan that nothing was driving, which reads as success to an operator.
//
// The supervisor-reachable path is covered by
// TestPlanImportUsesSupervisorWhenReachable below.
func TestPlanImportRefusesWithoutSupervisor(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)

	projectDir := t.TempDir()
	chdir(t, projectDir)

	planPath := filepath.Join(projectDir, "plan.md")
	if err := os.WriteFile(planPath, []byte("# Ship It\n\n1. do the thing\n"), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	cmd := newTestRootCmd(context.Background())
	cmd.SetArgs([]string{"plan", "import", planPath})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("plan import succeeded with no supervisor; it must fail closed")
	}
	if !errors.Is(err, errNoSupervisorListening) {
		t.Fatalf("error = %v, want errNoSupervisorListening", err)
	}
	// The refusal must tell the operator how to proceed.
	if !strings.Contains(err.Error(), "start one with") {
		t.Errorf("error %q does not tell the operator how to start a supervisor", err)
	}

	// Nothing may have been written: a refused import must leave no plan behind.
	st, err := store.Open(context.Background(), store.Options{DSN: store.DSN(storeDBPath(stateDir))})
	if err != nil {
		// No store file at all is an equally valid outcome — the client never
		// created one, which is the point.
		return
	}
	defer func() { _ = st.Close() }()
	plans, err := st.ListPlans(context.Background(), "", nil)
	if err == nil && len(plans) != 0 {
		t.Fatalf("refused import left %d plans behind", len(plans))
	}
}

func TestPlanImportEmptyFileRejected(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)
	projectDir := t.TempDir()
	chdir(t, projectDir)

	planPath := filepath.Join(projectDir, "empty.md")
	if err := os.WriteFile(planPath, []byte("   \n\n"), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	cmd := newTestRootCmd(context.Background())
	cmd.SetArgs([]string{"plan", "import", planPath})
	if err := cmd.Execute(); err == nil {
		t.Fatal("plan import of an empty file: want error, got nil")
	}
}

func TestPlanImportRejectsAmbiguousGrammarBeforeCreatingProjectOrPlan(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", stateDir)
	projectDir := t.TempDir()
	chdir(t, projectDir)

	planPath := filepath.Join(projectDir, "ambiguous.md")
	raw := "# Ambiguous\n\nThis leading paragraph could be a step.\n\n- actual step\n"
	if err := os.WriteFile(planPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	cmd := newTestRootCmd(context.Background())
	cmd.SetArgs([]string{"plan", "import", planPath})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "paragraph before its list") {
		t.Fatalf("plan import ambiguous error = %v, want grammar finding", err)
	}

	st, openErr := store.Open(context.Background(), store.Options{DSN: store.DSN(storeDBPath(stateDir))})
	if openErr != nil {
		t.Fatalf("open store: %v", openErr)
	}
	defer func() { _ = st.Close() }()
	plans, listErr := st.ListPlans(context.Background(), "", []store.PlanStatus{
		store.PlanStatusDraft,
		store.PlanStatusActive,
	})
	if listErr != nil {
		t.Fatalf("ListPlans: %v", listErr)
	}
	if len(plans) != 0 {
		t.Fatalf("invalid import created plans: %+v", plans)
	}
}

// TestPlanImportDuplicateSlugRejected confirms a second import with the same
// derived slug fails cleanly (the store's duplicate-slug guard) rather than
// silently creating two plans.
// TestPlanImportDuplicateSlugRejectedViaSupervisor keeps duplicate-slug
// coverage now that import always goes through the supervisor. The rejection
// itself is the store's unique-slug constraint; what this proves is that the
// error survives the IPC round trip instead of being swallowed.
func TestPlanImportDuplicateSlugRejectedViaSupervisor(t *testing.T) {
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
	if err := os.WriteFile(planPath, []byte("# Same Title\n\n1. step\n"), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	first := newTestRootCmd(context.Background())
	first.SetArgs([]string{"plan", "import", planPath})
	if err := first.Execute(); err != nil {
		t.Fatalf("first import: %v", err)
	}
	second := newTestRootCmd(context.Background())
	second.SetArgs([]string{"plan", "import", planPath})
	if err := second.Execute(); err == nil {
		t.Fatal("second import with the same slug: want error, got nil")
	}

	cancel()
	<-done
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

	cmd := newTestRootCmd(context.Background())
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
