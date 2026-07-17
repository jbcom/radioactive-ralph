package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func mustCreatePlan(t *testing.T, s *Store, projectID, slug string) string {
	t.Helper()
	id, err := s.CreatePlan(context.Background(), CreatePlanOpts{
		ProjectID: projectID,
		Slug:      slug,
		Title:     slug,
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	return id
}

// mustCreateSessionAndWorker creates a session + worker row so tests can
// exercise the FK-constrained claimed_by_session / claimed_by_worker_id
// columns without needing to care about session/worker lifecycle details.
func mustCreateSessionAndWorker(t *testing.T, s *Store, tag string) (sessionID, workerID string) {
	t.Helper()
	ctx := context.Background()
	sessionID, err := s.CreateSession(ctx, SessionOpts{
		Role: "supervisor", PID: 1, PIDStartTime: "t0-" + tag,
	})
	if err != nil {
		t.Fatalf("CreateSession(%s): %v", tag, err)
	}
	workerID, err = s.CreateWorker(ctx, WorkerOpts{
		SessionID: sessionID, Provider: "claude",
		SubprocessPID: 100, SubprocessStartTime: "t0-" + tag,
	})
	if err != nil {
		t.Fatalf("CreateWorker(%s): %v", tag, err)
	}
	return sessionID, workerID
}

func TestCreateTaskAndReady(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "task-project")
	planID := mustCreatePlan(t, s, projectID, "task-plan")

	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "a", Description: "first"}); err != nil {
		t.Fatalf("CreateTask a: %v", err)
	}
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "b", Description: "second"}); err != nil {
		t.Fatalf("CreateTask b: %v", err)
	}
	if err := s.AddDep(ctx, planID, "b", "a"); err != nil {
		t.Fatalf("AddDep: %v", err)
	}

	ready, err := s.Ready(ctx, planID)
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if len(ready) != 1 || ready[0].ID != "a" {
		t.Fatalf("Ready = %+v, want only task a", ready)
	}
}

func TestAddDepRejectsSelfAndCycle(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "cycle-project")
	planID := mustCreatePlan(t, s, projectID, "cycle-plan")

	for _, id := range []string{"a", "b", "c"} {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: id, Description: id}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}

	if err := s.AddDep(ctx, planID, "a", "a"); err == nil {
		t.Error("AddDep self-dep: want error, got nil")
	}

	if err := s.AddDep(ctx, planID, "b", "a"); err != nil {
		t.Fatalf("AddDep b->a: %v", err)
	}
	if err := s.AddDep(ctx, planID, "c", "b"); err != nil {
		t.Fatalf("AddDep c->b: %v", err)
	}
	// a -> c would close the cycle a -> c -> b -> a.
	if err := s.AddDep(ctx, planID, "a", "c"); err == nil {
		t.Error("AddDep creating cycle: want error, got nil")
	}
}

func TestClaimNextReadyAndMarkDone(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "claim-project")
	planID := mustCreatePlan(t, s, projectID, "claim-plan")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	_, worker2ID := mustCreateSessionAndWorker(t, s, "2")

	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "a", Description: "first"}); err != nil {
		t.Fatalf("CreateTask a: %v", err)
	}
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "b", Description: "second"}); err != nil {
		t.Fatalf("CreateTask b: %v", err)
	}
	if err := s.AddDep(ctx, planID, "b", "a"); err != nil {
		t.Fatalf("AddDep: %v", err)
	}

	task, err := s.ClaimNextReady(ctx, planID, sessionID, workerID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if task.ID != "a" {
		t.Fatalf("ClaimNextReady claimed %q, want a", task.ID)
	}
	if task.Status != TaskStatusRunning {
		t.Errorf("claimed task status = %q, want running", task.Status)
	}
	if task.ClaimedByWorkerID != workerID {
		t.Errorf("claimed task worker = %q, want %q", task.ClaimedByWorkerID, workerID)
	}

	// b is not ready yet — a hasn't completed.
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, worker2ID); !errors.Is(err, ErrNoReadyTask) {
		t.Errorf("ClaimNextReady before a done: err = %v, want ErrNoReadyTask", err)
	}

	newlyReady, err := s.MarkDone(ctx, planID, "a", sessionID, `{"exit_code":0}`)
	if err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
	if len(newlyReady) != 1 || newlyReady[0].ID != "b" {
		t.Fatalf("MarkDone newly ready = %+v, want task b", newlyReady)
	}

	got, err := s.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask a: %v", err)
	}
	if got.Status != TaskStatusDone {
		t.Errorf("task a status = %q, want done", got.Status)
	}
	if got.ClaimedByWorkerID != "" {
		t.Errorf("task a claimed_by_worker_id = %q, want empty after done", got.ClaimedByWorkerID)
	}
}

// TestClaimNextReadyConcurrentUniqueness is the load-bearing correctness
// test for the whole claim path: N goroutines race to claim from a pool of
// M single-task-capacity tasks. With _txlock=immediate + a checked
// RowsAffected, every task must be claimed by EXACTLY ONE goroutine — never
// zero, never two.
func TestClaimNextReadyConcurrentUniqueness(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "concurrent-project")
	planID := mustCreatePlan(t, s, projectID, "concurrent-plan")

	const numTasks = 20
	for i := 0; i < numTasks; i++ {
		id := taskIDForIndex(i)
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: id, Description: id}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}

	const numWorkers = 8
	sessionIDs := make([]string, numWorkers)
	workerIDs := make([]string, numWorkers)
	for w := 0; w < numWorkers; w++ {
		sessionIDs[w], workerIDs[w] = mustCreateSessionAndWorker(t, s, fmt.Sprintf("w%d", w))
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed = map[string]int{} // task id -> number of goroutines that successfully claimed it
	)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerN int) {
			defer wg.Done()
			for {
				task, err := s.ClaimNextReady(ctx, planID, sessionIDs[workerN], workerIDs[workerN])
				if errors.Is(err, ErrNoReadyTask) {
					return
				}
				if err != nil {
					t.Errorf("ClaimNextReady: %v", err)
					return
				}
				mu.Lock()
				claimed[task.ID]++
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()

	if len(claimed) != numTasks {
		t.Fatalf("claimed %d distinct tasks, want %d: %v", len(claimed), numTasks, claimed)
	}
	for id, n := range claimed {
		if n != 1 {
			t.Errorf("task %s claimed %d times, want exactly 1", id, n)
		}
	}
}

func taskIDForIndex(i int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	return string(letters[i%len(letters)]) + fmt.Sprintf("%d", i/len(letters))
}

func TestMarkFailedRetriesThenTerminal(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "fail-project")
	planID := mustCreatePlan(t, s, projectID, "fail-plan")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")

	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "a", Description: "first"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	for i := 0; i < 2; i++ {
		if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
			t.Fatalf("ClaimNextReady iteration %d: %v", i, err)
		}
		retried, err := s.MarkFailed(ctx, planID, "a", sessionID, "boom", 2)
		if err != nil {
			t.Fatalf("MarkFailed iteration %d: %v", i, err)
		}
		if !retried {
			t.Fatalf("MarkFailed iteration %d: retried = false, want true", i)
		}
	}

	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady final: %v", err)
	}
	retried, err := s.MarkFailed(ctx, planID, "a", sessionID, "boom again", 2)
	if err != nil {
		t.Fatalf("MarkFailed final: %v", err)
	}
	if retried {
		t.Error("MarkFailed final: retried = true, want false (out of retries)")
	}

	got, err := s.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusFailed {
		t.Errorf("task status = %q, want failed", got.Status)
	}
}

func TestMarkBlocked(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "block-project")
	planID := mustCreatePlan(t, s, projectID, "block-plan")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")

	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "a", Description: "first"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if err := s.MarkBlocked(ctx, planID, "a", sessionID, EventPayload{Reason: "needs input"}); err != nil {
		t.Fatalf("MarkBlocked: %v", err)
	}

	got, err := s.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusBlocked {
		t.Errorf("task status = %q, want blocked", got.Status)
	}
	if got.ClaimedByWorkerID != "" {
		t.Errorf("claimed_by_worker_id = %q, want empty", got.ClaimedByWorkerID)
	}
}

func TestListTasksFiltersByStatus(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "list-task-project")
	planID := mustCreatePlan(t, s, projectID, "list-task-plan")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")

	for _, id := range []string{"a", "b"} {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: id, Description: id}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	running, err := s.ListTasks(ctx, planID, []TaskStatus{TaskStatusRunning})
	if err != nil {
		t.Fatalf("ListTasks(running): %v", err)
	}
	if len(running) != 1 {
		t.Fatalf("ListTasks(running) = %+v, want 1", running)
	}

	all, err := s.ListTasks(ctx, planID, nil)
	if err != nil {
		t.Fatalf("ListTasks(nil): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListTasks(nil) = %+v, want 2", all)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "gettask-notfound-project")
	planID := mustCreatePlan(t, s, projectID, "gettask-notfound-plan")

	if _, err := s.GetTask(ctx, planID, "does-not-exist"); err == nil {
		t.Error("GetTask for missing task: want error, got nil")
	}
}

func TestGetTaskFound(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "gettask-found-project")
	planID := mustCreatePlan(t, s, projectID, "gettask-found-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "a", Description: "first"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	got, err := s.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Description != "first" {
		t.Errorf("Description = %q, want first", got.Description)
	}
	if got.Status != TaskStatusPending {
		t.Errorf("Status = %q, want pending", got.Status)
	}
}

// TestClaimNextReadyHonorsSequenceOrdinal confirms two independently-ready
// tasks (no dep relationship) are claimed in sequence_ordinal order, not
// creation order, when both have an explicit ordinal set.
func TestClaimNextReadyHonorsSequenceOrdinal(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "seqord-project")
	planID := mustCreatePlan(t, s, projectID, "seqord-plan")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")

	second := int64(2)
	first := int64(1)
	// Created in "b, a" order but sequence_ordinal says a (1) then b (2).
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "b", Description: "second", SequenceOrdinal: &second}); err != nil {
		t.Fatalf("CreateTask b: %v", err)
	}
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "a", Description: "first", SequenceOrdinal: &first}); err != nil {
		t.Fatalf("CreateTask a: %v", err)
	}

	task, err := s.ClaimNextReady(ctx, planID, sessionID, workerID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if task.ID != "a" {
		t.Fatalf("ClaimNextReady claimed %q, want a (sequence_ordinal=1 before 2)", task.ID)
	}
}

// TestClaimNextReadyNoTasksInPlan confirms claiming against a plan with no
// tasks at all returns ErrNoReadyTask cleanly (the SELECT's zero-row case,
// distinct from "tasks exist but none are ready").
func TestClaimNextReadyNoTasksInPlan(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "empty-plan-project")
	planID := mustCreatePlan(t, s, projectID, "empty-plan")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")

	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); !errors.Is(err, ErrNoReadyTask) {
		t.Errorf("ClaimNextReady on empty plan: err = %v, want ErrNoReadyTask", err)
	}
}
