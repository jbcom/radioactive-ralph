package orch

import (
	"context"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// blockingRunner signals when Run begins and then blocks until its context is
// cancelled, returning the context error. It lets a test prove KillWorker
// actually cancels the in-flight provider turn.
type blockingRunner struct {
	started chan string // receives the run's working dir when Run begins
}

func (r *blockingRunner) Run(ctx context.Context, _ provider.Binding, req provider.Request) (provider.Result, error) {
	select {
	case r.started <- req.WorkingDir:
	default:
	}
	<-ctx.Done()
	return provider.Result{}, ctx.Err()
}

// TestKillWorkerCancelsInFlightRun proves the process half of worker-kill: a
// dispatched worker whose provider turn is blocking gets its run context
// cancelled by KillWorker, so the run returns promptly instead of hanging until
// its own timeout. Before the cancellation registry, KillWorker had no handle
// on the live run and the subprocess would keep going.
func TestKillWorkerCancelsInFlightRun(t *testing.T) {
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "kill-project")
	planID := mustCreateTestPlan(t, s, projectID, "kill-plan", "Ship", twoStepSequentialPlan)

	runner := &blockingRunner{started: make(chan string, 1)}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	// Dispatch now launches the provider turn in its own goroutine and returns
	// promptly (the never-block invariant), so DispatchNext itself does not block
	// on the runner.
	if _, err := o.DispatchNext(context.Background(), projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}

	// Wait for the run to actually begin, then discover the live worker id and
	// kill it.
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("runner never started")
	}

	workerID := waitForRegisteredWorker(t, o)
	if !o.KillWorker(workerID) {
		t.Fatalf("KillWorker(%s) = false, want true (a run should be registered)", workerID)
	}

	// Drain the in-flight dispatch goroutine: the kill cancelled the run, so this
	// returns promptly rather than after the 5-min stall timeout.
	done := make(chan struct{})
	go func() { o.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatched worker did not finish after KillWorker cancelled the run")
	}

	// After the run is cancelled and the goroutine has finished, the worker is
	// deregistered, so a second kill reports no live run.
	if o.KillWorker(workerID) {
		t.Errorf("KillWorker(%s) after run ended = true, want false (should be deregistered)", workerID)
	}
}

// TestKillWorkerUnknownReturnsFalse confirms killing an id with no live run is a
// harmless false.
func TestKillWorkerUnknownReturnsFalse(t *testing.T) {
	o := New(newTestStore(t))
	if o.KillWorker("no-such-worker") {
		t.Error("KillWorker(unknown) = true, want false")
	}
}

// waitForRegisteredWorker polls the orchestrator's cancellation registry until
// exactly one worker is registered and returns its id.
func waitForRegisteredWorker(t *testing.T, o *Orchestrator) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		o.runningWorkersMu.Lock()
		var id string
		for k := range o.runningWorkers {
			id = k
		}
		n := len(o.runningWorkers)
		o.runningWorkersMu.Unlock()
		if n == 1 {
			return id
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no worker registered within 3s")
	return ""
}

// TestDispatchNextDoesNotBlockOnSlowProvider is the core proof of the never-block
// invariant: DispatchNext launches the provider turn asynchronously, so even
// when the turn blocks indefinitely, DispatchNext returns promptly (it does NOT
// wait for runner.Run). Before the async-dispatch fix, runner.Run ran inline and
// DispatchNext blocked for up to the stall timeout, wedging the supervisor's
// dispatchMu (and thus the tick, HandleEnqueue, and the reaper).
func TestDispatchNextDoesNotBlockOnSlowProvider(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "slow-project")
	planID := mustCreateTestPlan(t, s, projectID, "slow-plan", "Ship", twoStepSequentialPlan)

	runner := &blockingRunner{started: make(chan string, 1)}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	done := make(chan int, 1)
	go func() {
		n, err := o.DispatchNext(ctx, projectID, planID)
		if err != nil {
			t.Errorf("DispatchNext: %v", err)
		}
		done <- n
	}()

	select {
	case n := <-done:
		if n != 1 {
			t.Fatalf("dispatched = %d, want 1", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DispatchNext blocked on the slow provider turn — the never-block invariant is violated")
	}

	// The provider turn is still blocked (proving DispatchNext returned WITHOUT
	// waiting for it). Cancel it and drain so the test doesn't leak a goroutine.
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("provider turn never started")
	}
	workerID := waitForRegisteredWorker(t, o)
	o.KillWorker(workerID)
	o.Wait()
}

// TestDispatchSemaphoreBoundsInFlightTurns proves the maxParallel bound survives
// the move to async dispatch: with WithMaxParallel(1) and a blocking runner, only
// ONE provider turn is in flight at a time even across multiple dispatch passes,
// because the semaphore is not released until the running turn finishes.
func TestDispatchSemaphoreBoundsInFlightTurns(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "sem-project")
	planID := mustCreateTestPlan(t, s, projectID, "sem-plan", "Fan", threeStepParallelPlan)

	runner := &blockingRunner{started: make(chan string, 8)}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
		WithMaxParallel(1),
	)

	// First pass: maxParallel=1 caps this to a single dispatched (and now blocked)
	// worker even though three steps are ready.
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext #1: %v", err)
	}
	// A second pass must dispatch NOTHING: the one slot is held by the still-
	// blocked turn, so acquireDispatchSlot fails and the pass is a no-op.
	n2, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext #2: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second pass dispatched %d, want 0 (the single slot is occupied by the blocked turn)", n2)
	}

	// Exactly one turn started.
	<-runner.started
	select {
	case <-runner.started:
		t.Fatal("a second provider turn started despite maxParallel=1")
	case <-time.After(200 * time.Millisecond):
	}

	// Cancel the blocked turn and drain.
	workerID := waitForRegisteredWorker(t, o)
	o.KillWorker(workerID)
	o.Wait()
}

// blockingRunner must satisfy provider.Runner.
var _ provider.Runner = (*blockingRunner)(nil)
