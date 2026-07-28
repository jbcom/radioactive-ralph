package store

import (
	"context"
	"testing"
)

// seedReadyGraph imports a small plan graph directly, so these tests exercise
// the readiness walk without going through orch's markdown import.
func seedReadyGraph(t *testing.T, s *Store, projectID, slug string, specs []GraphTaskSpec) string {
	t.Helper()
	planID, err := s.CreatePlanGraph(context.Background(), CreatePlanGraphOpts{
		CreatePlanOpts: CreatePlanOpts{
			ProjectID:      projectID,
			Slug:           slug,
			Title:          "Graph",
			SourceMarkdown: "# Graph\n\n1. seeded\n",
		},
		Tasks:    specs,
		Activate: true,
	})
	if err != nil {
		t.Fatalf("CreatePlanGraph: %v", err)
	}
	return planID
}

func readySpec(id, group string, deps ...string) GraphTaskSpec {
	return GraphTaskSpec{
		CreateTaskOpts: CreateTaskOpts{ID: id, Description: id},
		DependsOn:      deps,
		GroupPath:      group,
		MetadataJSON:   "{}",
	}
}

func partitionTaskIDs(p ReadyPartition) []string {
	ids := make([]string, 0, len(p.Tasks))
	for _, t := range p.Tasks {
		ids = append(ids, t.ID)
	}
	return ids
}

// TestReadyPartitionsGroupsByPersistedGroupPath is the point of the increment:
// a ready wave is not "n independent tasks", it is a set of partitions, each a
// leaf group that native fan-out may delegate to ONE provider. Two tasks being
// simultaneously ready says nothing about whether they belong to one group.
func TestReadyPartitionsGroupsByPersistedGroupPath(t *testing.T) {
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "ready-partition")
	planID := seedReadyGraph(t, s, projectID, "wave", []GraphTaskSpec{
		readySpec("a", "0"),
		readySpec("b", "0"),
		readySpec("c", "1"),
	})

	parts, err := s.ReadyPartitions(context.Background(), planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("got %d partitions, want 2: %+v", len(parts), parts)
	}
	if parts[0].GroupPath != "0" || len(parts[0].Tasks) != 2 {
		t.Errorf("partition 0 = %q %v, want group \"0\" with two tasks",
			parts[0].GroupPath, partitionTaskIDs(parts[0]))
	}
	if parts[1].GroupPath != "1" || len(parts[1].Tasks) != 1 {
		t.Errorf("partition 1 = %q %v, want group \"1\" with one task",
			parts[1].GroupPath, partitionTaskIDs(parts[1]))
	}
}

// TestReadyPartitionsNeverMergesDistinctGroups pins the specific bug group_path
// exists to prevent: dispatch fanning out because len(ready) > 1, handing one
// provider two tasks from different leaf groups — and therefore different
// headings, bindings, and independence domains.
func TestReadyPartitionsNeverMergesDistinctGroups(t *testing.T) {
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "ready-distinct")
	planID := seedReadyGraph(t, s, projectID, "distinct", []GraphTaskSpec{
		readySpec("x", "0.0"),
		readySpec("y", "0.1"),
	})

	parts, err := s.ReadyPartitions(context.Background(), planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("two ready tasks in different leaf groups produced %d partitions, want 2", len(parts))
	}
	for _, p := range parts {
		if len(p.Tasks) != 1 {
			t.Errorf("partition %q holds %v, want exactly one task", p.GroupPath, partitionTaskIDs(p))
		}
	}
}

// TestReadyPartitionsHonorsEdges proves partitioning sits on top of the edge
// walk rather than replacing it: a dependent task is absent until its
// predecessor completes, then appears in its OWN group's partition.
func TestReadyPartitionsHonorsEdges(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "ready-edges")
	planID := seedReadyGraph(t, s, projectID, "edges", []GraphTaskSpec{
		readySpec("first", "0"),
		readySpec("second", "1", "first"),
	})

	parts, err := s.ReadyPartitions(ctx, planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	if len(parts) != 1 || parts[0].GroupPath != "0" {
		t.Fatalf("before completion got %+v, want only group 0", parts)
	}

	sessionID, workerID := mustCreateSessionAndWorker(t, s, "edges")
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if _, err := s.MarkDone(ctx, planID, "first", sessionID, "{}"); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	parts, err = s.ReadyPartitions(ctx, planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	if len(parts) != 1 || parts[0].GroupPath != "1" || len(parts[0].Tasks) != 1 {
		t.Fatalf("after completion got %+v, want only group 1 with one task", parts)
	}
	if parts[0].Tasks[0].ID != "second" {
		t.Errorf("ready task = %q, want second", parts[0].Tasks[0].ID)
	}
}

// TestReadyPartitionsExcludesApprovalGated keeps ReadyPartitions consistent
// with Ready: a task held behind the approval gate is deliberately not
// dispatchable, and must not inflate a partition that fan-out would then
// delegate wholesale to one provider.
func TestReadyPartitionsExcludesApprovalGated(t *testing.T) {
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "ready-gated")
	gated := readySpec("gated", "0")
	gated.RequiresApproval = true
	planID := seedReadyGraph(t, s, projectID, "gated", []GraphTaskSpec{
		readySpec("open", "0"),
		gated,
	})

	parts, err := s.ReadyPartitions(context.Background(), planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("got %d partitions, want 1", len(parts))
	}
	if len(parts[0].Tasks) != 1 || parts[0].Tasks[0].ID != "open" {
		t.Fatalf("partition holds %v, want only the ungated task", partitionTaskIDs(parts[0]))
	}
}

// TestReadyPartitionsSurfacesFailClosedBlockedTasks pins the asymmetry between
// the two kinds of block, which is easy to "tidy" into a bug in either
// direction.
//
// A fail-closed block (blocked_capability, blocked_input) is released by the
// DISPATCH-TIME GATE that imposed it: the gate re-checks, finds the operator has
// fixed the binding or the declaration, and clears the block. A task this walk
// never returns never reaches its gate, so excluding these states makes a block
// outlive its own remedy — a permanent stall dressed as a gate.
//
// An approval gate is the opposite: only a human decision recorded out of band
// releases it, so returning it would hand dispatch a task it must re-block every
// tick, forever, with nothing gained.
func TestReadyPartitionsSurfacesFailClosedBlockedTasks(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		status TaskStatus
		want   bool
		why    string
	}{
		{"capability", TaskStatusBlockedCapability, true,
			"the capability gate clears this block when the binding is fixed, and it only runs on tasks the walk returns"},
		{"input", TaskStatusBlockedInput, true,
			"the path gate clears this block when the declaration is fixed, and it only runs on tasks the walk returns"},
		{"approval", TaskStatusReadyPendingApproval, false,
			"only an out-of-band human decision releases an approval gate, so re-running dispatch on it can never help"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := openTestStore(t)
			projectID := mustCreateProject(t, s, "ready-blocked-"+tc.name)
			planID := seedReadyGraph(t, s, projectID, "blocked-"+tc.name, []GraphTaskSpec{
				readySpec("subject", "0"),
			})
			if _, err := s.db.ExecContext(ctx,
				`UPDATE tasks SET status = ? WHERE plan_id = ? AND id = 'subject'`,
				string(tc.status), planID); err != nil {
				t.Fatalf("set status %s: %v", tc.status, err)
			}

			parts, err := s.ReadyPartitions(ctx, planID)
			if err != nil {
				t.Fatalf("ReadyPartitions: %v", err)
			}
			var found bool
			for _, p := range parts {
				for _, task := range p.Tasks {
					if task.ID == "subject" {
						found = true
					}
				}
			}
			if found != tc.want {
				t.Fatalf("surfaced %s = %v, want %v — %s", tc.status, found, tc.want, tc.why)
			}
		})
	}
}

// TestReadyPartitionsTasksWithoutMetadata covers tasks created by the plain
// CreateTask path (no task_metadata row): they must still be returned, under
// the empty group path, rather than vanishing from dispatch on a JOIN miss. A
// vanished task is an unrunnable plan.
func TestReadyPartitionsTasksWithoutMetadata(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "ready-bare")
	planID, err := s.CreatePlan(ctx, CreatePlanOpts{
		ProjectID: projectID, Slug: "plain", Title: "Plain",
		SourceMarkdown: "# Plain\n\n1. one\n",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "bare", Description: "bare"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	parts, err := s.ReadyPartitions(ctx, planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	if len(parts) != 1 || len(parts[0].Tasks) != 1 || parts[0].Tasks[0].ID != "bare" {
		t.Fatalf("got %+v, want one partition holding the metadata-less task", parts)
	}
	if parts[0].GroupPath != "" {
		t.Errorf("GroupPath = %q, want empty for a task with no metadata row", parts[0].GroupPath)
	}
}
