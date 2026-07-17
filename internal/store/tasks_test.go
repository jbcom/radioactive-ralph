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

func TestStatusCounts(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "counts-project")
	activePlan := mustCreatePlan(t, s, projectID, "active-plan")
	if err := s.SetPlanStatus(ctx, activePlan, PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus active: %v", err)
	}
	// A second, paused plan must NOT be counted as active.
	pausedPlan := mustCreatePlan(t, s, projectID, "paused-plan")
	if err := s.SetPlanStatus(ctx, pausedPlan, PlanStatusPaused); err != nil {
		t.Fatalf("SetPlanStatus paused: %v", err)
	}

	// Seed tasks across the statuses the reply surfaces, plus a 'done' and a
	// 'pending' that must NOT appear in any of the counted fields.
	seed := map[string]TaskStatus{
		"r1":  TaskStatusReady,
		"r2":  TaskStatusReady,
		"run": TaskStatusRunning,
		"ap":  TaskStatusReadyPendingApproval,
		"bl":  TaskStatusBlocked,
		"f1":  TaskStatusFailed,
		"dn":  TaskStatusDone,
		"pd":  TaskStatusPending,
	}
	for id, status := range seed {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: activePlan, ID: id, Description: id}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
		if _, err := s.DB().ExecContext(ctx, `UPDATE tasks SET status=? WHERE plan_id=? AND id=?`, string(status), activePlan, id); err != nil {
			t.Fatalf("set status %s: %v", id, err)
		}
	}

	c, err := s.StatusCounts(ctx)
	if err != nil {
		t.Fatalf("StatusCounts: %v", err)
	}
	want := StatusCounts{ActivePlans: 1, Ready: 2, Running: 1, Approval: 1, Blocked: 1, Failed: 1}
	if c != want {
		t.Errorf("StatusCounts = %+v, want %+v", c, want)
	}
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

// TestMarkFailedIgnoresStaleReportFromReclaimedSession is the regression for
// the audit's high-severity finding: a failure report from a worker whose
// claim was already reclaimed (and reassigned to a new session) must NOT
// stomp the new owner. MarkFailed is guarded on the reporting session still
// owning the running task; a stale report returns ErrTaskNotOwnedRunning and
// changes nothing.
func TestMarkFailedIgnoresStaleReportFromReclaimedSession(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "stale-project")
	planID := mustCreatePlan(t, s, projectID, "stale-plan")
	sessionA, workerA := mustCreateSessionAndWorker(t, s, "A")
	sessionB, workerB := mustCreateSessionAndWorker(t, s, "B")

	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "t", Description: "work"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Worker A claims the task, then the reaper reclaims it (back to pending,
	// claim cleared) — simulated here with the same UPDATE ReclaimStale runs —
	// then worker B claims it and is now the running owner.
	if _, err := s.ClaimNextReady(ctx, planID, sessionA, workerA); err != nil {
		t.Fatalf("A ClaimNextReady: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE tasks SET status = 'pending', reclaim_count = reclaim_count + 1,
		                 claimed_by_session = NULL, claimed_by_worker_id = NULL
		WHERE plan_id = ? AND id = ?`, planID, "t"); err != nil {
		t.Fatalf("simulate reclaim: %v", err)
	}
	claimedByB, err := s.ClaimNextReady(ctx, planID, sessionB, workerB)
	if err != nil {
		t.Fatalf("B ClaimNextReady: %v", err)
	}
	if claimedByB.ID != "t" {
		t.Fatalf("B claimed %q, want t", claimedByB.ID)
	}

	// A's stale failure report lands: it must be a benign no-op, NOT a stomp.
	retried, err := s.MarkFailed(ctx, planID, "t", sessionA, "A's late crash", 3)
	if !errors.Is(err, ErrTaskNotOwnedRunning) {
		t.Fatalf("stale MarkFailed err = %v, want ErrTaskNotOwnedRunning", err)
	}
	if retried {
		t.Error("stale MarkFailed reported retried=true; must be a no-op")
	}

	// The task must still be running under B, with B's claim intact.
	got, err := s.GetTask(ctx, planID, "t")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusRunning {
		t.Errorf("task status = %q, want running (B's claim must survive A's stale report)", got.Status)
	}
	if got.ClaimedBySession != sessionB {
		t.Errorf("claimed_by_session = %q, want B's session %q", got.ClaimedBySession, sessionB)
	}

	// B's own MarkFailed still works (owner guard matches).
	if _, err := s.MarkFailed(ctx, planID, "t", sessionB, "B's real failure", 3); err != nil {
		t.Fatalf("B MarkFailed (real owner): %v", err)
	}
}

// TestCreateTaskDuplicateIsDistinguishable is the regression for the audit's
// swallowed-error finding: CreateTask must return ErrDuplicateTask (not a
// generic error) on a (plan_id, id) collision, so callers can tolerate the
// benign race while still surfacing real insert failures.
func TestCreateTaskDuplicateIsDistinguishable(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "dup-project")
	planID := mustCreatePlan(t, s, projectID, "dup-plan")

	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "x", Description: "first"}); err != nil {
		t.Fatalf("first CreateTask: %v", err)
	}
	err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "x", Description: "again"})
	if !errors.Is(err, ErrDuplicateTask) {
		t.Fatalf("duplicate CreateTask err = %v, want ErrDuplicateTask", err)
	}
}

// TestReleaseClaimDoesNotChargeRetry is the regression for CodeRabbit's
// finding that a system-level release (e.g. an aborted fan-out group) must
// requeue a task WITHOUT charging its retry budget — unlike MarkFailed.
func TestReleaseClaimDoesNotChargeRetry(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "release-project")
	planID := mustCreatePlan(t, s, projectID, "release-plan")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")

	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "t", Description: "work"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	if err := s.ReleaseClaim(ctx, planID, "t", sessionID, "aborted"); err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}

	got, err := s.GetTask(ctx, planID, "t")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusPending {
		t.Errorf("status = %q, want pending after release", got.Status)
	}
	if got.RetryCount != 0 {
		t.Errorf("retry_count = %d, want 0 — a system release must not charge a retry", got.RetryCount)
	}
	if got.ClaimedBySession != "" {
		t.Errorf("claim not cleared: claimed_by_session = %q", got.ClaimedBySession)
	}

	// A stale release (wrong session) is a benign no-op.
	if err := s.ReleaseClaim(ctx, planID, "t", "some-other-session", "stale"); !errors.Is(err, ErrTaskNotOwnedRunning) {
		t.Errorf("stale ReleaseClaim err = %v, want ErrTaskNotOwnedRunning", err)
	}
}

// TestMarkDoneIgnoresStaleCompletionFromReclaimedSession is the regression
// for the second-pass audit's high finding: the acceptance path must carry
// the SAME owner guard as the failure path, or a stale completion from a
// reclaimed+reassigned worker marks the task done with the wrong evidence
// and clears the new owner's live claim.
func TestMarkDoneIgnoresStaleCompletionFromReclaimedSession(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "markdone-stale-project")
	planID := mustCreatePlan(t, s, projectID, "markdone-stale-plan")
	sessionA, workerA := mustCreateSessionAndWorker(t, s, "A")
	sessionB, workerB := mustCreateSessionAndWorker(t, s, "B")

	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "t", Description: "work"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// A claims; reaper reclaims; B re-claims and is the live owner.
	if _, err := s.ClaimNextReady(ctx, planID, sessionA, workerA); err != nil {
		t.Fatalf("A claim: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE tasks SET status='pending', reclaim_count=reclaim_count+1,
		                 claimed_by_session=NULL, claimed_by_worker_id=NULL
		WHERE plan_id=? AND id=?`, planID, "t"); err != nil {
		t.Fatalf("simulate reclaim: %v", err)
	}
	if _, err := s.ClaimNextReady(ctx, planID, sessionB, workerB); err != nil {
		t.Fatalf("B claim: %v", err)
	}

	// A's stale completion must be a benign no-op (ErrTaskNotOwnedRunning),
	// leaving the task running under B.
	if _, err := s.MarkDone(ctx, planID, "t", sessionA, `{"exit_code":0}`); !errors.Is(err, ErrTaskNotOwnedRunning) {
		t.Fatalf("stale MarkDone err = %v, want ErrTaskNotOwnedRunning", err)
	}
	got, err := s.GetTask(ctx, planID, "t")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != TaskStatusRunning || got.ClaimedBySession != sessionB {
		t.Errorf("A's stale MarkDone stomped B: status=%q owner=%q, want running under B", got.Status, got.ClaimedBySession)
	}

	// B's real completion still works.
	if _, err := s.MarkDone(ctx, planID, "t", sessionB, `{"exit_code":0}`); err != nil {
		t.Fatalf("B MarkDone (real owner): %v", err)
	}
}

func TestApproveTask(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "approve-project")
	planID := mustCreatePlan(t, s, projectID, "approve-plan")

	// Create a task and force it into ready_pending_approval.
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "t", Description: "gated"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status='ready_pending_approval' WHERE plan_id=? AND id=?`, planID, "t"); err != nil {
		t.Fatalf("set gate: %v", err)
	}

	found, changed, err := s.ApproveTask(ctx, planID, "t")
	if err != nil || !found || !changed {
		t.Fatalf("ApproveTask = (found=%v changed=%v err=%v), want (true,true,nil)", found, changed, err)
	}
	got, _ := s.GetTask(ctx, planID, "t")
	if got.Status != TaskStatusReady {
		t.Errorf("status = %q, want ready after approve", got.Status)
	}

	// Idempotent: approving again is a benign no-change success.
	found, changed, err = s.ApproveTask(ctx, planID, "t")
	if err != nil || !found || changed {
		t.Errorf("second ApproveTask = (found=%v changed=%v err=%v), want (true,false,nil)", found, changed, err)
	}

	// Unknown task → found=false, no error.
	found, _, err = s.ApproveTask(ctx, planID, "nope")
	if err != nil || found {
		t.Errorf("ApproveTask(unknown) = (found=%v err=%v), want (false,nil)", found, err)
	}
}

func TestReclaimWorker(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "reclaim-project")
	planID := mustCreatePlan(t, s, projectID, "reclaim-plan")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")

	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "t", Description: "work"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if err := s.SetWorkerTask(ctx, workerID, planID, "t"); err != nil {
		t.Fatalf("SetWorkerTask: %v", err)
	}

	found, err := s.ReclaimWorker(ctx, workerID)
	if err != nil || !found {
		t.Fatalf("ReclaimWorker = (found=%v err=%v), want (true,nil)", found, err)
	}
	// Task requeued to pending; worker terminated.
	got, _ := s.GetTask(ctx, planID, "t")
	if got.Status != TaskStatusPending {
		t.Errorf("task status = %q, want pending after worker kill", got.Status)
	}
	if got.ClaimedByWorkerID != "" {
		t.Errorf("task still claimed by worker %q after kill", got.ClaimedByWorkerID)
	}

	// Unknown worker → found=false, no error.
	found, err = s.ReclaimWorker(ctx, "no-such-worker")
	if err != nil || found {
		t.Errorf("ReclaimWorker(unknown) = (found=%v err=%v), want (false,nil)", found, err)
	}
}
