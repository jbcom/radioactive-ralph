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

	// Dispatch on a goroutine; it will block inside the runner until killed.
	dispatchDone := make(chan error, 1)
	go func() {
		_, err := o.DispatchNext(context.Background(), projectID, planID)
		dispatchDone <- err
	}()

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

	select {
	case err := <-dispatchDone:
		// DispatchNext returns nil even when the run errored — a worker
		// terminating (here, via cancellation) is handled as a failed turn, not
		// a DispatchNext error. The point is that it RETURNED promptly.
		_ = err
	case <-time.After(3 * time.Second):
		t.Fatal("DispatchNext did not return after KillWorker cancelled the run")
	}

	// After the run is cancelled, the worker is deregistered, so a second kill
	// reports no live run.
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

// blockingRunner must satisfy provider.Runner.
var _ provider.Runner = (*blockingRunner)(nil)
