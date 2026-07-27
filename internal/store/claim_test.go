package store

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestClaimTaskClaimsExactlyTheNamedTask is the property that distinguishes
// ClaimTask from ClaimNextReady. ClaimNextReady returns whichever ready task its
// ORDER BY surfaces, which forced the orchestrator to reconcile a substituted
// task afterward. With explicit edges the dispatcher knows what it wants, so a
// claim for "b" must return "b" even when "a" is also ready and sorts first.
func TestClaimTaskClaimsExactlyTheNamedTask(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "named-claim")
	planID := mustCreatePlan(t, s, projectID, "p-named")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	for _, id := range []string{"a", "b"} {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: id, Description: id}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}

	task, err := s.ClaimTask(ctx, planID, "b", sessionID, workerID)
	if err != nil {
		t.Fatalf("ClaimTask b: %v", err)
	}
	if task.ID != "b" {
		t.Fatalf("claimed %q, want exactly %q", task.ID, "b")
	}
	if task.Status != TaskStatusRunning {
		t.Errorf("status = %q, want running", task.Status)
	}

	// "a" must be untouched — a named claim takes one task, not a wave.
	other, err := s.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask a: %v", err)
	}
	if other.Status != TaskStatusPending {
		t.Errorf("task a status = %q, want pending (untouched)", other.Status)
	}
}

// TestClaimTaskRefusesUnreadyTask covers the dependency predicate: a task whose
// dependency is unsatisfied is not claimable even when named explicitly.
// Claiming it would run work whose inputs do not exist yet.
func TestClaimTaskRefusesUnreadyTask(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "named-unready")
	planID := mustCreatePlan(t, s, projectID, "p-unready")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	session2ID, worker2ID := mustCreateSessionAndWorker(t, s, "2")
	for _, id := range []string{"first", "second"} {
		if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: id, Description: id}); err != nil {
			t.Fatalf("CreateTask %s: %v", id, err)
		}
	}
	if err := s.AddDep(ctx, planID, "second", "first"); err != nil {
		t.Fatalf("AddDep: %v", err)
	}

	if _, err := s.ClaimTask(ctx, planID, "second", sessionID, workerID); !errors.Is(err, ErrNoReadyTask) {
		t.Fatalf("ClaimTask on a blocked task = %v, want ErrNoReadyTask", err)
	}

	// Satisfy the dependency; the same named claim must now succeed.
	if _, err := s.ClaimTask(ctx, planID, "first", sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask first: %v", err)
	}
	if _, err := s.MarkDone(ctx, planID, "first", sessionID, "{}"); err != nil {
		t.Fatalf("MarkDone first: %v", err)
	}
	task, err := s.ClaimTask(ctx, planID, "second", session2ID, worker2ID)
	if err != nil {
		t.Fatalf("ClaimTask second after dependency done: %v", err)
	}
	if task.ID != "second" {
		t.Fatalf("claimed %q, want second", task.ID)
	}
}

// TestClaimTaskConcurrentUniqueness is the safety property: many workers racing
// for ONE named task must produce exactly one winner. A second winner would mean
// two providers running the same task against the same working tree.
func TestClaimTaskConcurrentUniqueness(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "named-race")
	planID := mustCreatePlan(t, s, projectID, "p-race")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "only", Description: "only"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	const workers = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	var unexpected []error

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ClaimTask(ctx, planID, "only", sessionID, workerID)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, ErrNoReadyTask):
				// Expected for every loser.
			default:
				unexpected = append(unexpected, err)
			}
		}()
	}
	wg.Wait()

	for _, err := range unexpected {
		t.Errorf("unexpected claim error: %v", err)
	}
	if wins != 1 {
		t.Fatalf("claim winners = %d, want exactly 1", wins)
	}
}

// TestClaimTaskRefusesAlreadyRunningTask closes the double-dispatch path: a task
// already claimed by another session must not be re-claimable by name.
func TestClaimTaskRefusesAlreadyRunningTask(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "named-running")
	planID := mustCreatePlan(t, s, projectID, "p-running")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	session2ID, worker2ID := mustCreateSessionAndWorker(t, s, "2")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "t", Description: "t"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := s.ClaimTask(ctx, planID, "t", sessionID, workerID); err != nil {
		t.Fatalf("first ClaimTask: %v", err)
	}
	if _, err := s.ClaimTask(ctx, planID, "t", session2ID, worker2ID); !errors.Is(err, ErrNoReadyTask) {
		t.Fatalf("second ClaimTask = %v, want ErrNoReadyTask", err)
	}
}

// TestClaimTaskRefusesUnknownTask fails closed on a task id that does not exist,
// rather than silently substituting some other ready task.
func TestClaimTaskRefusesUnknownTask(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "named-unknown")
	planID := mustCreatePlan(t, s, projectID, "p-unknown")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "real", Description: "real"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := s.ClaimTask(ctx, planID, "ghost", sessionID, workerID); !errors.Is(err, ErrNoReadyTask) {
		t.Fatalf("ClaimTask on an unknown id = %v, want ErrNoReadyTask", err)
	}
	// The real ready task must not have been claimed as a substitute.
	remaining, err := s.GetTask(ctx, planID, "real")
	if err != nil {
		t.Fatalf("GetTask real: %v", err)
	}
	if remaining.Status != TaskStatusPending {
		t.Errorf("task real status = %q, want pending — a named claim must never substitute", remaining.Status)
	}
}

// TestClaimTaskClaimsApprovedReadyTask mirrors the ClaimNextReady guard that
// once stranded approved tasks: the UPDATE's status set must match the SELECT's,
// or a task in 'ready' passes selection and then fails the claim.
func TestClaimTaskClaimsApprovedReadyTask(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "named-approved")
	planID := mustCreatePlan(t, s, projectID, "p-approved")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	if err := s.CreateTask(ctx, CreateTaskOpts{
		PlanID: planID, ID: "gated", Description: "gated", RequiresApproval: true,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// A task awaiting approval must not be claimable.
	if _, err := s.ClaimTask(ctx, planID, "gated", sessionID, workerID); !errors.Is(err, ErrNoReadyTask) {
		t.Fatalf("ClaimTask on an unapproved task = %v, want ErrNoReadyTask", err)
	}
	found, changed, err := s.ApproveTask(ctx, planID, "gated")
	if err != nil {
		t.Fatalf("ApproveTask: %v", err)
	}
	if !found || !changed {
		t.Fatalf("ApproveTask found=%v changed=%v, want both true", found, changed)
	}
	task, err := s.ClaimTask(ctx, planID, "gated", sessionID, workerID)
	if err != nil {
		t.Fatalf("ClaimTask after approval: %v", err)
	}
	if task.ID != "gated" {
		t.Fatalf("claimed %q, want gated", task.ID)
	}
}
