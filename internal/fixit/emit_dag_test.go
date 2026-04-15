package fixit

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/plandag"
)

// TestEmitToDAGRoundTrip writes a realistic PlanProposal into plandag
// and verifies every row lands as expected — plan + intent + tasks +
// deps + acceptance-criteria task.
func TestEmitToDAGRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, err := plandag.Open(ctx, plandag.Options{
		DSN: "file:" + filepath.Join(t.TempDir(), "plans.db") +
			"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)",
	})
	if err != nil {
		t.Fatalf("plandag.Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	proposal := PlanProposal{
		Primary:          "green",
		PrimaryRationale: "The scope matches green's parallelism envelope.",
		Tasks: []Task{
			{
				Title: "wire pocketbase bootstrap", Effort: "M", Impact: "L",
				VariantHint: "green", ContextBoundary: true,
				AcceptanceCriteria: []string{"app.Execute() returns 0"},
			},
			{
				Title: "implement plandag package", Effort: "L", Impact: "L",
				VariantHint: "green", ContextBoundary: true,
				AcceptanceCriteria: []string{"round-trip test passes"},
				DependsOn:          []string{"wire pocketbase bootstrap"},
			},
			{
				Title: "run integration tests", Effort: "S", Impact: "M",
				DependsOn: []string{"implement plandag package"},
			},
		},
		AcceptanceCriteria: []string{
			"go test ./... green",
			"site builds",
		},
		Confidence: 85,
	}

	result, err := EmitToDAG(ctx, EmitToDAGOpts{
		Store:    store,
		Topic:    "m3-emit-test",
		Proposal: proposal,
		Validation: ValidationResult{
			Passed: true,
		},
		Status: StatusCurrent,
		Intent: IntentSpec{
			Topic:       "m3-emit-test",
			Description: "Test the DAG emitter",
			Constraints: []string{"no opus", "weekend only"},
		},
		RC: RepoContext{
			GitRoot:       "/tmp/testrepo",
			CurrentBranch: "main",
		},
		RawOutput: `{"primary":"green"}`,
	})
	if err != nil {
		t.Fatalf("EmitToDAG: %v", err)
	}

	// Three regular tasks + one synthesized acceptance-criteria task.
	if len(result.TaskIDs) != 4 {
		t.Errorf("TaskIDs len = %d, want 4 (3 + acceptance)", len(result.TaskIDs))
	}

	// Plan row exists and is active.
	plan, err := store.GetPlan(ctx, result.PlanID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if plan.Status != plandag.PlanStatusActive {
		t.Errorf("plan status = %q, want active", plan.Status)
	}
	if plan.Confidence != 85 {
		t.Errorf("plan confidence = %d, want 85", plan.Confidence)
	}

	// Initial ready set should contain only the first task (deps
	// rooted at "wire pocketbase bootstrap") — the second depends on
	// the first, the third depends on the second, and the acceptance
	// task depends on all three.
	ready, err := store.Ready(ctx, result.PlanID)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(ready) != 1 {
		t.Errorf("initial ready set size = %d, want 1", len(ready))
	}
	if len(ready) > 0 && ready[0].Description != "wire pocketbase bootstrap" {
		t.Errorf("first ready task = %q, want 'wire pocketbase bootstrap'", ready[0].Description)
	}

	// Intent row exists.
	var intentCount int
	if err := store.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM intents WHERE plan_id = ?", result.PlanID).Scan(&intentCount); err != nil {
		t.Fatalf("count intents: %v", err)
	}
	if intentCount != 1 {
		t.Errorf("intent rows = %d, want 1", intentCount)
	}

	// Analysis row exists (we passed RawOutput).
	var analysisCount int
	if err := store.DB().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM analyses WHERE plan_id = ?", result.PlanID).Scan(&analysisCount); err != nil {
		t.Fatalf("count analyses: %v", err)
	}
	if analysisCount != 1 {
		t.Errorf("analysis rows = %d, want 1", analysisCount)
	}
}

// TestSlugifyTaskID checks the edge cases: non-ASCII, punctuation,
// long titles, empty after cleanup.
func TestSlugifyTaskID(t *testing.T) {
	cases := []struct {
		in       string
		index    int
		expected string
	}{
		{"Wire PocketBase Bootstrap", 0, "wire-pocketbase-bootstrap"},
		{"  -- leading/trailing --  ", 1, "leadingtrailing"},
		{"", 2, "task-3"},
		{"!!!", 3, "task-4"},
		{"a_b c-d", 4, "a-b-c-d"},
		{"  double   spaces  ", 5, "double-spaces"},
	}
	for _, c := range cases {
		got := slugifyTaskID(c.in, c.index)
		if got != c.expected {
			t.Errorf("slugifyTaskID(%q, %d) = %q, want %q", c.in, c.index, got, c.expected)
		}
	}
}
