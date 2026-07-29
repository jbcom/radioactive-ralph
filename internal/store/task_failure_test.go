package store

import (
	"context"
	"testing"
)

// TestFailedTaskKeepsItsFailureCategory closes a P2 review finding on the
// failure-reason work: the reason lived ONLY in the event log, and the operator
// snapshot returns a bounded event page (20 by default). A task can sit
// terminal indefinitely while newer activity from other tasks evicts the
// evidence for why it died, leaving the row a bare "failed" again.
//
// Persisting the category on the task makes the answer independent of how much
// unrelated activity has happened since.
func TestFailedTaskKeepsItsFailureCategory(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "task-failure-category")
	planID := seedReadyGraph(t, s, projectID, "dies", []GraphTaskSpec{
		readySpec("build", "0"),
	})
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	if _, err := s.ClaimTask(ctx, planID, "build", sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if _, err := s.MarkFailedWithPayload(ctx, planID, "build", sessionID,
		EventPayload{Reason: "boom", FailureCategory: "auth"}, 0); err != nil {
		t.Fatalf("MarkFailedWithPayload: %v", err)
	}

	items, err := operatorTasksForTest(ctx, s, projectID)
	if err != nil {
		t.Fatalf("operator tasks: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d task(s), want 1", len(items))
	}
	if got := items[0].Status; got != TaskStatusFailed {
		t.Fatalf("fixture wrong: build is %q, want failed", got)
	}
	if got := items[0].FailureCategory; got != "auth" {
		t.Errorf("FailureCategory = %q, want \"auth\": the reason lives only in "+
			"the event log, so it is lost the moment the failure event ages out "+
			"of the bounded snapshot page", got)
	}
}

// TestRetriedTaskCarriesNoFailureCategory keeps the field describing the
// CURRENT state. A retry returns the task to pending, and a pending task that
// still advertised its last attempt's failure would read as broken when it is
// simply queued to run again.
func TestRetriedTaskCarriesNoFailureCategory(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "task-failure-retry")
	planID := seedReadyGraph(t, s, projectID, "retries", []GraphTaskSpec{
		readySpec("build", "0"),
	})
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "1")
	if _, err := s.ClaimTask(ctx, planID, "build", sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	retried, err := s.MarkFailedWithPayload(ctx, planID, "build", sessionID,
		EventPayload{Reason: "transient", FailureCategory: "rate_limit"}, 3)
	if err != nil {
		t.Fatalf("MarkFailedWithPayload: %v", err)
	}
	if !retried {
		t.Fatal("fixture wrong: expected a retry with maxRetries=3")
	}

	items, err := operatorTasksForTest(ctx, s, projectID)
	if err != nil {
		t.Fatalf("operator tasks: %v", err)
	}
	if got := items[0].FailureCategory; got != "" {
		t.Errorf("a REQUEUED task reports FailureCategory=%q; it is pending, not "+
			"failed, and advertising the last attempt's failure makes a healthy "+
			"retry read as broken", got)
	}
}
