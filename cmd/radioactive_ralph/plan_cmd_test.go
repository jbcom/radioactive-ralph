package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

func TestDerivePlanTitleUsesFirstHeading(t *testing.T) {
	got := derivePlanTitle("# Rebuild the runtime\n\nsome body\n", "/plans/x.md")
	if got != "Rebuild the runtime" {
		t.Errorf("title = %q, want %q", got, "Rebuild the runtime")
	}
}

func TestDerivePlanTitleFallsBackToFilename(t *testing.T) {
	got := derivePlanTitle("no heading here\n", "/plans/my-plan.md")
	if got != "my-plan" {
		t.Errorf("title = %q, want %q (filename sans extension)", got, "my-plan")
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Rebuild the Runtime":  "rebuild-the-runtime",
		"  Trailing/Leading  ": "trailing-leading",
		"UPPER_and_snake":      "upper-and-snake",
		"a---b":                "a-b",
		"!!!":                  "plan",
		"Ship v2.0 (final)":    "ship-v2-0-final",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
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
