package plandag

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

// openPlanStoreForTest is a tiny helper used by this file's tests.
// Other plandag tests inline Open — kept local so this file stays
// self-contained.
func openPlanStoreForTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), Options{
		DSN: "file:" + filepath.Join(t.TempDir(), "plans.db"),
	})
	if err != nil {
		t.Fatalf("plandag.Open: %v", err)
	}
	return s
}

func TestTaskDepsBothDirections(t *testing.T) {
	ctx := context.Background()
	store := openPlanStoreForTest(t)
	t.Cleanup(func() { _ = store.Close() })

	planID, err := store.CreatePlan(ctx, CreatePlanOpts{
		Slug:     "dep-test",
		Title:    "dep test",
		RepoPath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// DAG shape: a → b, a → c, b → d, c → d
	//
	//     a
	//    / \
	//   b   c
	//    \ /
	//     d
	for _, id := range []string{"a", "b", "c", "d"} {
		if err := store.CreateTask(ctx, CreateTaskOpts{
			PlanID:      planID,
			ID:          id,
			Description: id,
		}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}
	for _, edge := range [][2]string{{"b", "a"}, {"c", "a"}, {"d", "b"}, {"d", "c"}} {
		if err := store.AddDep(ctx, planID, edge[0], edge[1]); err != nil {
			t.Fatalf("AddDep %v: %v", edge, err)
		}
	}

	cases := []struct {
		task     string
		depsOn   []string
		dependBy []string
	}{
		{"a", nil, []string{"b", "c"}},
		{"b", []string{"a"}, []string{"d"}},
		{"c", []string{"a"}, []string{"d"}},
		{"d", []string{"b", "c"}, nil},
	}
	for _, c := range cases {
		got, err := store.TaskDeps(ctx, planID, c.task)
		if err != nil {
			t.Fatalf("TaskDeps(%s): %v", c.task, err)
		}
		if !reflect.DeepEqual(got.DependsOn, c.depsOn) {
			t.Errorf("task %s: DependsOn = %v, want %v", c.task, got.DependsOn, c.depsOn)
		}
		if !reflect.DeepEqual(got.DependedBy, c.dependBy) {
			t.Errorf("task %s: DependedBy = %v, want %v", c.task, got.DependedBy, c.dependBy)
		}
	}
}

func TestTaskDepsEmptyForLoneTask(t *testing.T) {
	ctx := context.Background()
	store := openPlanStoreForTest(t)
	t.Cleanup(func() { _ = store.Close() })

	planID, err := store.CreatePlan(ctx, CreatePlanOpts{
		Slug: "lone", Title: "lone", RepoPath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := store.CreateTask(ctx, CreateTaskOpts{
		PlanID: planID, ID: "only", Description: "only",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := store.TaskDeps(ctx, planID, "only")
	if err != nil {
		t.Fatalf("TaskDeps: %v", err)
	}
	if len(got.DependsOn) != 0 || len(got.DependedBy) != 0 {
		t.Errorf("lone task has edges: %+v", got)
	}
}
