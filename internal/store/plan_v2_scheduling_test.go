package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRetryBlockedTaskIsNarrowAndObservable(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	projectID := mustCreateProject(t, s, "retry-blocked")
	planID, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		Plan: CreatePlanOpts{ProjectID: projectID, Slug: "retry", Title: "Retry"},
		Tasks: []GraphTaskSpec{
			{ID: "input", Description: "input", TeamPath: "studio", MetadataJSON: `{}`},
			{ID: "capability", Description: "capability", TeamPath: "studio", MetadataJSON: `{}`},
			{ID: "ordinary", Description: "ordinary", TeamPath: "studio", MetadataJSON: `{}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkBlockedInput(ctx, planID, "input", "stale hash"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkBlockedCapability(ctx, planID, "capability", "pool unavailable"); err != nil {
		t.Fatal(err)
	}
	for _, taskID := range []string{"input", "capability"} {
		found, changed, err := s.RetryBlockedTask(ctx, planID, taskID)
		if err != nil || !found || !changed {
			t.Fatalf("RetryBlockedTask(%s) = %v,%v,%v", taskID, found, changed, err)
		}
		task, _ := s.GetTask(ctx, planID, taskID)
		if task.Status != TaskStatusPending || task.BlockedReason != "" {
			t.Fatalf("retried task = %+v", task)
		}
		events, err := s.ListTaskEvents(ctx, planID, taskID, 10)
		if err != nil || len(events) == 0 || events[0].Kind != "task.requeued" {
			t.Fatalf("retry events = %+v, %v", events, err)
		}
	}
	if _, _, err := s.RetryBlockedTask(ctx, planID, "ordinary"); !errors.Is(err, ErrTaskNotRetryable) {
		t.Fatalf("ordinary retry err = %v", err)
	}
	if found, _, err := s.RetryBlockedTask(ctx, planID, "missing"); err != nil || found {
		t.Fatalf("missing retry = found %v, err %v", found, err)
	}
}

func TestV2PlanConvergesOnlyAfterEveryTaskSatisfied(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	projectID := mustCreateProject(t, s, "convergence")
	planID, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		Plan:   CreatePlanOpts{ProjectID: projectID, Slug: "converge", Title: "Converge"},
		Status: PlanStatusActive,
		Tasks: []GraphTaskSpec{
			{ID: "one", Description: "one", TeamPath: "studio", MetadataJSON: `{}`},
			{ID: "two", Description: "two", TeamPath: "studio", MetadataJSON: `{}`, DependsOn: []string{"one"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, worker := mustCreateSessionAndWorker(t, s, "converge-one")
	if _, err := s.ClaimReadyTask(ctx, planID, "one", session, worker); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkDone(ctx, planID, "one", session, `{}`); err != nil {
		t.Fatal(err)
	}
	if plan, _ := s.GetPlan(ctx, planID); plan.Status != PlanStatusActive {
		t.Fatalf("partial plan status = %s", plan.Status)
	}
	if err := s.MarkBlockedCapability(ctx, planID, "two", "wait"); err != nil {
		t.Fatal(err)
	}
	if plan, _ := s.GetPlan(ctx, planID); plan.Status != PlanStatusActive {
		t.Fatalf("blocked plan status = %s", plan.Status)
	}
	if _, _, err := s.RetryBlockedTask(ctx, planID, "two"); err != nil {
		t.Fatal(err)
	}
	session, worker = mustCreateSessionAndWorker(t, s, "converge-two")
	if _, err := s.ClaimReadyTask(ctx, planID, "two", session, worker); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkDone(ctx, planID, "two", session, `{}`); err != nil {
		t.Fatal(err)
	}
	if plan, _ := s.GetPlan(ctx, planID); plan.Status != PlanStatusDone {
		t.Fatalf("converged plan status = %s", plan.Status)
	}
}

func TestCrossPlanReadWriteReservations(t *testing.T) {
	tests := []struct {
		name          string
		firstInput    bool
		secondInput   bool
		secondBlocked bool
	}{
		{name: "reader reader", firstInput: true, secondInput: true},
		{name: "reader writer", firstInput: true, secondBlocked: true},
		{name: "writer reader", secondInput: true, secondBlocked: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := openTestStore(t)
			ctx := context.Background()
			projectID := mustCreateProject(t, s, "rw-"+tt.name)
			create := func(slug string, input bool) string {
				spec := GraphTaskSpec{
					ID: slug, Description: slug, TeamPath: "studio", MetadataJSON: `{}`,
				}
				if input {
					spec.Inputs = []InputReservation{{Path: "shared/data", SHA256: fmt.Sprintf("%064d", 0)}}
				} else {
					spec.Outputs = []OutputReservation{{Path: "shared/data", Mode: "exclusive"}}
				}
				id, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
					Plan:  CreatePlanOpts{ProjectID: projectID, Slug: slug, Title: slug},
					Tasks: []GraphTaskSpec{spec},
				})
				if err != nil {
					t.Fatal(err)
				}
				return id
			}
			firstPlan := create("first", tt.firstInput)
			secondPlan := create("second", tt.secondInput)
			session1, worker1 := mustCreateSessionAndWorker(t, s, "rw-first")
			if _, err := s.ClaimReadyTask(ctx, firstPlan, "first", session1, worker1); err != nil {
				t.Fatal(err)
			}
			session2, worker2 := mustCreateSessionAndWorker(t, s, "rw-second")
			_, err := s.ClaimReadyTask(ctx, secondPlan, "second", session2, worker2)
			if tt.secondBlocked && !errors.Is(err, ErrOutputReserved) {
				t.Fatalf("second claim err = %v, want reservation conflict", err)
			}
			if !tt.secondBlocked && err != nil {
				t.Fatalf("shared reader claim: %v", err)
			}
		})
	}
}

func TestConcurrentExactClaimsReserveOneWriterOutOfTwoHundred(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	projectID := mustCreateProject(t, s, "race-200")
	const racers = 200
	type raceCandidate struct{ plan, task, session, worker string }
	candidates := make([]raceCandidate, racers)
	for i := range racers {
		taskID := fmt.Sprintf("task-%03d", i)
		planID, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
			Plan: CreatePlanOpts{
				ProjectID: projectID, Slug: fmt.Sprintf("race-%03d", i), Title: taskID,
			},
			Tasks: []GraphTaskSpec{{
				ID: taskID, Description: taskID, TeamPath: "studio", MetadataJSON: `{}`,
				Outputs: []OutputReservation{{Path: "shared/result.json", Mode: "exclusive"}},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		session, worker := mustCreateSessionAndWorker(t, s, taskID)
		candidates[i] = raceCandidate{planID, taskID, session, worker}
	}
	var successes atomic.Int64
	var unexpected atomic.Int64
	unexpectedErrors := make(chan error, racers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, candidate := range candidates {
		wg.Add(1)
		go func(candidate raceCandidate) {
			defer wg.Done()
			<-start
			_, err := s.ClaimReadyTask(
				ctx, candidate.plan, candidate.task, candidate.session, candidate.worker,
			)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrOutputReserved):
			default:
				unexpected.Add(1)
				unexpectedErrors <- err
			}
		}(candidate)
	}
	close(start)
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("successful claims = %d, want 1", got)
	}
	if got := unexpected.Load(); got != 0 {
		close(unexpectedErrors)
		var details []string
		for err := range unexpectedErrors {
			details = append(details, err.Error())
		}
		t.Fatalf("unexpected claim errors = %d: %s", got, strings.Join(details, " | "))
	}
}

func TestTeamRollupsIncludeHierarchicalBlockedAndProviderState(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	projectID := mustCreateProject(t, s, "team-rollup")
	planID, err := s.CreatePlanGraph(ctx, CreatePlanGraphOpts{
		Plan: CreatePlanOpts{ProjectID: projectID, Slug: "teams", Title: "Teams"},
		Tasks: []GraphTaskSpec{
			{ID: "story", Description: "story", TeamPath: "studio/narrative", MetadataJSON: `{}`},
			{ID: "art", Description: "art", TeamPath: "studio/art/pixel", MetadataJSON: `{}`},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkBlockedCapability(ctx, planID, "story", "no causal reviewer"); err != nil {
		t.Fatal(err)
	}
	session, worker := mustCreateSessionAndWorker(t, s, "team-art")
	if _, err := s.ClaimReadyTask(ctx, planID, "art", session, worker); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTaskExecution(
		ctx, planID, "art", "opencode-qwen", "opencode",
		"ollama/qwen3.5:4b", "default", "ollama-macmini", session,
	); err != nil {
		t.Fatal(err)
	}
	rollups, err := s.TeamRollups(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]TeamRollup{}
	for _, rollup := range rollups {
		byPath[rollup.TeamPath] = rollup
	}
	if got := byPath["studio"]; got.Total != 2 || got.Blocked != 1 ||
		got.Running != 1 || got.ActiveWorkers != 1 || got.Providers["opencode-qwen"] != 1 {
		t.Fatalf("studio rollup = %+v", got)
	}
	if got := byPath["studio/art"]; got.Total != 1 || got.Running != 1 {
		t.Fatalf("studio/art rollup = %+v", got)
	}
}
