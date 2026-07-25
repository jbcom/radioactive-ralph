package store

import (
	"context"
	"errors"
	"testing"
)

func TestCreatePlanGraphAtomicallyMaterializesStableTasksAndDeps(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	projectID := mustCreateProject(t, s, "plan-v2")

	planID, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		Plan: CreatePlanOpts{
			ProjectID: projectID, Slug: "v2", Title: "V2", SourceMarkdown: "# V2",
		},
		Tasks: []GraphTaskSpec{
			{ID: "audit.story", Description: "audit", TeamPath: "design/story", MetadataJSON: `{"id":"audit.story"}`},
			{ID: "synth.story", Description: "synth", TeamPath: "design/story", MetadataJSON: `{"id":"synth.story"}`, DependsOn: []string{"audit.story"}, Outputs: []OutputReservation{{Path: "out/story.json", Mode: "exclusive"}}},
		},
	})
	if err != nil {
		t.Fatalf("CreatePlanGraph: %v", err)
	}
	tasks, err := s.ListTasks(ctx, planID, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 2 || tasks[0].ID != "audit.story" || tasks[1].ID != "synth.story" {
		t.Fatalf("tasks = %+v", tasks)
	}
	ready, err := s.Ready(ctx, planID)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "audit.story" {
		t.Fatalf("ready = %+v", ready)
	}
	metadata, err := s.GetTaskExecutionMetadata(ctx, planID, "synth.story")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if metadata.TeamPath != "design/story" || metadata.MetadataJSON != `{"id":"synth.story"}` {
		t.Fatalf("metadata = %+v", metadata)
	}
}

func TestCreatePlanGraphRollsBackOnInvalidDependency(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	projectID := mustCreateProject(t, s, "plan-v2-invalid")
	_, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		Plan: CreatePlanOpts{ProjectID: projectID, Slug: "bad", Title: "Bad"},
		Tasks: []GraphTaskSpec{{
			ID: "task.one", Description: "one", TeamPath: "team/one",
			MetadataJSON: `{"id":"task.one"}`, DependsOn: []string{"missing"},
		}},
	})
	if err == nil {
		t.Fatal("CreatePlanGraph = nil, want error")
	}
	plans, listErr := s.ListPlans(ctx, projectID, []PlanStatus{PlanStatusDraft})
	if listErr != nil {
		t.Fatalf("ListPlans: %v", listErr)
	}
	if len(plans) != 0 {
		t.Fatalf("plans = %+v, want rollback", plans)
	}
}

func TestTaskExecutionProvenanceAndCapabilityBlock(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	projectID := mustCreateProject(t, s, "plan-v2-provenance")
	planID, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		Plan: CreatePlanOpts{ProjectID: projectID, Slug: "proof", Title: "Proof"},
		Tasks: []GraphTaskSpec{{
			ID: "task.one", Description: "one", TeamPath: "team/one",
			MetadataJSON: `{"id":"task.one"}`,
		}},
	})
	if err != nil {
		t.Fatalf("CreatePlanGraph: %v", err)
	}
	if err := s.MarkBlockedCapability(ctx, planID, "task.one", "no eligible provider"); err != nil {
		t.Fatalf("MarkBlockedCapability: %v", err)
	}
	task, err := s.GetTask(ctx, planID, "task.one")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != TaskStatusBlockedCapability {
		t.Fatalf("status = %s", task.Status)
	}
	metadata, err := s.GetTaskExecutionMetadata(ctx, planID, "task.one")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if metadata.BlockedReason != "no eligible provider" {
		t.Fatalf("blocked reason = %q", metadata.BlockedReason)
	}
	if err := s.RecordTaskProvider(ctx, planID, "task.one", "codex"); !errors.Is(err, ErrTaskNotRunning) {
		t.Fatalf("RecordTaskProvider error = %v, want ErrTaskNotRunning", err)
	}
}

func TestClaimReadyTaskNeverSubstitutesAnotherReadyTask(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	projectID := mustCreateProject(t, s, "exact-claim")
	planID := mustCreatePlan(t, s, projectID, "exact-claim")
	for _, id := range []string{"first", "second"} {
		if err := s.CreateTask(ctx, CreateTaskOpts{
			PlanID: planID, ID: id, Description: id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "exact")
	task, err := s.ClaimReadyTask(ctx, planID, "second", sessionID, workerID)
	if err != nil {
		t.Fatalf("ClaimReadyTask: %v", err)
	}
	if task.ID != "second" {
		t.Fatalf("claimed %q, want second", task.ID)
	}
	first, _ := s.GetTask(ctx, planID, "first")
	if first.Status != TaskStatusPending {
		t.Fatalf("first status = %s, want pending", first.Status)
	}
}

func TestClaimReadyTaskEnforcesCrossPlanExclusiveOutputs(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	projectID := mustCreateProject(t, s, "output-lock")
	create := func(slug, id, output string) string {
		planID, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
			Plan: CreatePlanOpts{ProjectID: projectID, Slug: slug, Title: slug},
			Tasks: []GraphTaskSpec{{
				ID: id, Description: id, TeamPath: "studio/code",
				MetadataJSON: `{"id":"` + id + `"}`,
				Outputs:      []OutputReservation{{Path: output, Mode: "exclusive"}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return planID
	}
	firstPlan := create("first-output", "first", "dist")
	secondPlan := create("second-output", "second", "dist/game.js")
	firstSession, firstWorker := mustCreateSessionAndWorker(t, s, "output-first")
	if _, err := s.ClaimReadyTask(ctx, firstPlan, "first", firstSession, firstWorker); err != nil {
		t.Fatal(err)
	}
	secondSession, secondWorker := mustCreateSessionAndWorker(t, s, "output-second")
	if _, err := s.ClaimReadyTask(
		ctx, secondPlan, "second", secondSession, secondWorker,
	); !errors.Is(err, ErrOutputReserved) {
		t.Fatalf("second claim error = %v, want ErrOutputReserved", err)
	}
	if _, err := s.MarkDone(ctx, firstPlan, "first", firstSession, `{}`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimReadyTask(ctx, secondPlan, "second", secondSession, secondWorker); err != nil {
		t.Fatalf("claim after release: %v", err)
	}
}
