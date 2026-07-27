package store

import (
	"context"
	"errors"
	"testing"
)

// reserveOutput declares taskID as the exclusive writer of path.
func reserveOutput(t *testing.T, s *Store, planID, taskID, path string) {
	t.Helper()
	if err := s.ReserveTaskOutput(context.Background(), planID, taskID, path, "exclusive"); err != nil {
		t.Fatalf("ReserveTaskOutput(%s, %s): %v", taskID, path, err)
	}
}

// TestClaimTaskRefusesReservedOutput is the point of the reservation table. Two
// tasks can be simultaneously READY — no edge between them, nothing in the
// dependency graph to order them — and still be unsafe to run concurrently
// because they write the same file. Readiness answers "are this task's
// dependencies satisfied", not "is it safe to run right now".
//
// The second claim must be refused while the first is running, and must succeed
// once it finishes.
func TestClaimTaskRefusesReservedOutput(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "reserved-output")
	planID := seedReadyGraph(t, s, projectID, "reserved", []GraphTaskSpec{
		readySpec("first", "0"),
		readySpec("second", "0"),
	})
	reserveOutput(t, s, planID, "first", "build/out.txt")
	reserveOutput(t, s, planID, "second", "build/out.txt")

	sessionA, workerA := mustCreateSessionAndWorker(t, s, "res-a")
	if _, err := s.ClaimTask(ctx, planID, "first", sessionA, workerA); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	sessionB, workerB := mustCreateSessionAndWorker(t, s, "res-b")
	_, err := s.ClaimTask(ctx, planID, "second", sessionB, workerB)
	if !errors.Is(err, ErrOutputReserved) {
		t.Fatalf("second claim err = %v, want ErrOutputReserved — two tasks writing "+
			"the same exclusive path must not run concurrently", err)
	}

	// Once the holder finishes, the path is free and the second task claims.
	if _, err := s.MarkDone(ctx, planID, "first", sessionA, "{}"); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
	if _, err := s.ClaimTask(ctx, planID, "second", sessionB, workerB); err != nil {
		t.Fatalf("claim after the holder finished: %v", err)
	}
}

// TestClaimTaskAllowsDistinctOutputs is the control. Reservations must only
// block on an actual overlap, or declaring outputs at all would serialize every
// plan that uses them.
func TestClaimTaskAllowsDistinctOutputs(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "distinct-output")
	planID := seedReadyGraph(t, s, projectID, "distinct", []GraphTaskSpec{
		readySpec("first", "0"),
		readySpec("second", "0"),
	})
	reserveOutput(t, s, planID, "first", "build/a.txt")
	reserveOutput(t, s, planID, "second", "build/b.txt")

	sessionA, workerA := mustCreateSessionAndWorker(t, s, "dist-a")
	if _, err := s.ClaimTask(ctx, planID, "first", sessionA, workerA); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	sessionB, workerB := mustCreateSessionAndWorker(t, s, "dist-b")
	if _, err := s.ClaimTask(ctx, planID, "second", sessionB, workerB); err != nil {
		t.Fatalf("second claim on a DIFFERENT path: %v", err)
	}
}

// TestClaimTaskIgnoresReservationsOfFinishedTasks keeps a completed task's
// reservation from blocking forever. A reservation is a lock held for the
// duration of a RUN, not a permanent assignment of a path to a task.
func TestClaimTaskIgnoresReservationsOfFinishedTasks(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "finished-holder")
	planID := seedReadyGraph(t, s, projectID, "finished", []GraphTaskSpec{
		readySpec("done-already", "0"),
		readySpec("wants-path", "0"),
	})
	reserveOutput(t, s, planID, "done-already", "shared.txt")
	reserveOutput(t, s, planID, "wants-path", "shared.txt")

	sessionA, workerA := mustCreateSessionAndWorker(t, s, "fin-a")
	if _, err := s.ClaimTask(ctx, planID, "done-already", sessionA, workerA); err != nil {
		t.Fatalf("claim holder: %v", err)
	}
	if _, err := s.MarkDone(ctx, planID, "done-already", sessionA, "{}"); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}

	sessionB, workerB := mustCreateSessionAndWorker(t, s, "fin-b")
	if _, err := s.ClaimTask(ctx, planID, "wants-path", sessionB, workerB); err != nil {
		t.Fatalf("claim after holder finished: %v — a reservation is held for the "+
			"duration of a run, not permanently", err)
	}
}

// TestClaimNextReadySkipsAReservedTask keeps the unnamed claim consistent with
// the named one. If ClaimNextReady could hand out a task whose output is
// reserved by a running peer, the reservation would only constrain callers that
// happened to claim by name.
func TestClaimNextReadySkipsAReservedTask(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "next-ready-reserved")
	planID := seedReadyGraph(t, s, projectID, "nextready", []GraphTaskSpec{
		readySpec("holder", "0"),
		readySpec("blocked", "0"),
	})
	reserveOutput(t, s, planID, "holder", "shared.txt")
	reserveOutput(t, s, planID, "blocked", "shared.txt")

	sessionA, workerA := mustCreateSessionAndWorker(t, s, "nr-a")
	claimed, err := s.ClaimTask(ctx, planID, "holder", sessionA, workerA)
	if err != nil {
		t.Fatalf("claim holder: %v", err)
	}
	if claimed.ID != "holder" {
		t.Fatalf("claimed %q, want holder", claimed.ID)
	}

	sessionB, workerB := mustCreateSessionAndWorker(t, s, "nr-b")
	_, err = s.ClaimNextReady(ctx, planID, sessionB, workerB)
	if !errors.Is(err, ErrNoReadyTask) {
		t.Fatalf("ClaimNextReady err = %v, want ErrNoReadyTask — the only other "+
			"ready task has a reserved output", err)
	}
}

// TestReadyPartitionsStillReportsAReservedTask draws the line between readiness
// and admission. A reserved task IS ready — its dependencies are satisfied — it
// simply cannot be claimed yet. Hiding it from the ready set would conflate the
// two and make a temporarily-unclaimable task look like an unsatisfied one.
func TestReadyPartitionsStillReportsAReservedTask(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "ready-vs-admission")
	planID := seedReadyGraph(t, s, projectID, "readyvsadm", []GraphTaskSpec{
		readySpec("holder", "0"),
		readySpec("waiter", "0"),
	})
	reserveOutput(t, s, planID, "holder", "shared.txt")
	reserveOutput(t, s, planID, "waiter", "shared.txt")

	sessionA, workerA := mustCreateSessionAndWorker(t, s, "rva-a")
	if _, err := s.ClaimTask(ctx, planID, "holder", sessionA, workerA); err != nil {
		t.Fatalf("claim holder: %v", err)
	}

	parts, err := s.ReadyPartitions(ctx, planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	var found bool
	for _, p := range parts {
		for _, task := range p.Tasks {
			if task.ID == "waiter" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("the reserved task vanished from the ready set; readiness is about " +
			"dependencies, admission is about reservations — conflating them makes a " +
			"temporarily-unclaimable task look unsatisfied")
	}
}
