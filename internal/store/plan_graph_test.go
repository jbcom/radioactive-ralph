package store

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func graphTask(id, desc, group string, deps ...string) GraphTaskSpec {
	return GraphTaskSpec{
		CreateTaskOpts: CreateTaskOpts{ID: id, Description: desc},
		DependsOn:      deps,
		GroupPath:      group,
		TeamPath:       "team/a",
		MetadataJSON:   "{}",
	}
}

// TestCreatePlanGraphWritesNodesEdgesAndMetadata is the happy path: one call
// produces a plan whose readiness the existing Ready query can already resolve.
func TestCreatePlanGraphWritesNodesEdgesAndMetadata(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "graph-happy")

	planID, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		CreatePlanOpts: CreatePlanOpts{ProjectID: projectID, Slug: "g", Title: "G"},
		Tasks: []GraphTaskSpec{
			graphTask("a", "first", "0.0"),
			graphTask("b", "second", "0.0", "a"),
		},
		Activate: true,
	})
	if err != nil {
		t.Fatalf("CreatePlanGraph: %v", err)
	}

	// Only "a" is ready: "b" waits on it. That readiness comes from the
	// task_deps walk that already shipped — the import just populates it.
	ready, err := s.Ready(ctx, planID)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "a" {
		t.Fatalf("ready = %v, want exactly [a]", taskIDs(ready))
	}

	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "b")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if meta.GroupPath != "0.0" || meta.TeamPath != "team/a" {
		t.Errorf("metadata = %+v, want the group/team recorded at import", meta)
	}

	plan, err := s.GetPlan(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if plan.Status != PlanStatusActive {
		t.Errorf("status = %q, want active — an imported plan is meant to run", plan.Status)
	}
}

// TestCreatePlanGraphRejectsCycleWithinOneImport is why the cycle check has to
// run on the transaction. Both edges are written by this same call, so a check
// reading only committed rows would not see a→b when validating b→a.
func TestCreatePlanGraphRejectsCycleWithinOneImport(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "graph-cycle")

	_, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		CreatePlanOpts: CreatePlanOpts{ProjectID: projectID, Slug: "cyc", Title: "Cyc"},
		Tasks: []GraphTaskSpec{
			graphTask("a", "first", "0.0", "b"),
			graphTask("b", "second", "0.0", "a"),
		},
	})
	if err == nil {
		t.Fatal("accepted a cycle formed entirely within one import")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %v, want it to name the cycle", err)
	}
	assertNoPlanRows(t, s, projectID)
}

// TestCreatePlanGraphFailureLeavesNoPlanRow is the atomicity property. A
// partial import would leave a draft plan whose slug then blocks the retry with
// ErrDuplicateSlug — a plan permanently undispatchable.
func TestCreatePlanGraphFailureLeavesNoPlanRow(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "graph-atomic")

	// The second task is invalid (no description), so the import fails after
	// the plan row and the first task have already been written.
	_, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		CreatePlanOpts: CreatePlanOpts{ProjectID: projectID, Slug: "same", Title: "Same"},
		Tasks: []GraphTaskSpec{
			graphTask("a", "first", "0.0"),
			{CreateTaskOpts: CreateTaskOpts{ID: "b", Description: ""}, GroupPath: "0.0"},
		},
	})
	if err == nil {
		t.Fatal("accepted a task with no description")
	}
	assertNoPlanRows(t, s, projectID)

	// The decisive check: the same slug must import cleanly afterward. If the
	// failed attempt had left its plan row behind, this would fail with
	// ErrDuplicateSlug and the plan could never be created.
	planID, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		CreatePlanOpts: CreatePlanOpts{ProjectID: projectID, Slug: "same", Title: "Same"},
		Tasks:          []GraphTaskSpec{graphTask("a", "first", "0.0")},
	})
	if err != nil {
		t.Fatalf("retry after a failed import: %v (a partial write blocked the slug)", err)
	}
	if planID == "" {
		t.Fatal("retry returned an empty plan id")
	}
}

// TestCreatePlanGraphAllowsForwardReferences covers a plan declaring `after`
// against a step written below it — legal in the grammar, and why edges are
// inserted only once every node exists.
func TestCreatePlanGraphAllowsForwardReferences(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "graph-forward")

	planID, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		CreatePlanOpts: CreatePlanOpts{ProjectID: projectID, Slug: "fwd", Title: "Fwd"},
		Tasks: []GraphTaskSpec{
			// "a" depends on "b", which is defined after it.
			graphTask("a", "first", "0.0", "b"),
			graphTask("b", "second", "0.0"),
		},
	})
	if err != nil {
		t.Fatalf("CreatePlanGraph with a forward reference: %v", err)
	}
	ready, err := s.Ready(ctx, planID)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "b" {
		t.Fatalf("ready = %v, want exactly [b]", taskIDs(ready))
	}
}

// TestCreatePlanGraphDuplicateSlugStillRejected keeps the existing guard: two
// real plans cannot share a slug within a project.
func TestCreatePlanGraphDuplicateSlugStillRejected(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "graph-dup")
	opts := CreatePlanGraphOpts{
		CreatePlanOpts: CreatePlanOpts{ProjectID: projectID, Slug: "dup", Title: "Dup"},
		Tasks:          []GraphTaskSpec{graphTask("a", "first", "0.0")},
	}
	if _, err := s.CreatePlanGraph(ctx, opts); err != nil {
		t.Fatalf("first import: %v", err)
	}
	if _, err := s.CreatePlanGraph(ctx, opts); !errors.Is(err, ErrDuplicateSlug) {
		t.Fatalf("second import = %v, want ErrDuplicateSlug", err)
	}
}

func assertNoPlanRows(t *testing.T, s *Store, projectID string) {
	t.Helper()
	var n int
	if err := s.DB().QueryRow(
		`SELECT COUNT(*) FROM plans WHERE project_id = ?`, projectID,
	).Scan(&n); err != nil {
		t.Fatalf("count plans: %v", err)
	}
	if n != 0 {
		t.Fatalf("a failed import left %d plan row(s) behind", n)
	}
}

func taskIDs(tasks []Task) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.ID)
	}
	return out
}
