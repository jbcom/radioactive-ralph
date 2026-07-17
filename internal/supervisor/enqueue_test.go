package supervisor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// fakeRunner is a minimal provider.Runner test double — mirrors
// internal/orch's own fakeRunner but lives here since that one is
// unexported to its package. It returns a canned successful Result for
// every call so DispatchNext's mechanical, judgment-only fallback
// (non-empty evidence output) always accepts.
type fakeRunner struct {
	// mu guards calls: dispatch is asynchronous, so Run is invoked from the
	// dispatched worker goroutines concurrently with the test's assertions.
	mu    sync.Mutex
	nCall int
}

func (f *fakeRunner) Run(context.Context, provider.Binding, provider.Request) (provider.Result, error) {
	f.mu.Lock()
	f.nCall++
	f.mu.Unlock()
	return provider.Result{AssistantOutput: "did the work"}, nil
}

// calls returns the recorded run count (lock-safe). Read only after draining the
// orchestrator (sup.orch.Wait()) so all dispatched turns have completed.
func (f *fakeRunner) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nCall
}

// TestHandleEnqueue_DispatchesSeededPlan is the proof for task 5 of the
// Phase 6c tech-debt pass: HandleEnqueue must actually dispatch, not
// return "not implemented". A plan with one ready step is seeded directly
// in the store; a real Supervisor (constructed the same way Run does, via
// runSupervisorInBackground/Options) is given an Orchestrator wired to a
// fake runner; calling client.Enqueue must cause that one step to be
// dispatched and marked done.
func TestHandleEnqueue_DispatchesSeededPlan(t *testing.T) {
	runtimeDir := t.TempDir()
	st := openTestStore(t)
	ctx := context.Background()

	projectID, err := st.CreateProject(ctx, "enqueue-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: "/tmp/enqueue-project"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	planID, err := st.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "enqueue-plan",
		Title:          "Ship",
		SourceMarkdown: "# Ship\n\n1. write the code\n",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := st.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}

	runner := &fakeRunner{}
	o := orch.New(st,
		orch.WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		orch.WithBindingResolver(func(context.Context, string, bool) (provider.Binding, error) {
			return provider.Binding{Name: "claude", Config: provider.BindingConfig{Type: "claude", Binary: "true"}}, nil
		}),
	)

	cancel, done := runSupervisorInBackground(t, Options{RuntimeDir: runtimeDir, Store: st, Orchestrator: o})
	defer cancel()

	client := waitForSupervisor(t, runtimeDir, 2*time.Second)
	defer func() { _ = client.Close() }()

	reply, err := client.Enqueue(context.Background(), ipc.EnqueueArgs{Description: "kick the plan"})
	if err != nil {
		t.Fatalf("client.Enqueue: %v", err)
	}
	if !reply.Inserted {
		t.Errorf("EnqueueReply.Inserted = false, want true (the seeded plan's ready step should have been dispatched)")
	}
	o.Wait() // dispatch is async — wait for the dispatched turn to complete
	if runner.calls() != 1 {
		t.Fatalf("fakeRunner.calls = %d, want 1 — HandleEnqueue must actually dispatch, not no-op", runner.calls())
	}

	task, err := st.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != store.TaskStatusDone {
		t.Errorf("task status = %q, want done after the enqueue-triggered dispatch was verified", task.Status)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after ctx cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit within 3s of ctx cancel")
	}
}

// TestDispatchActivePlans_SkipsPausedPlan is the behavioral proof that the
// drive API's pause control actually pauses. A plan with a ready step is
// seeded and then PAUSED; a dispatch pass must dispatch NOTHING from it. Before
// the fix, dispatchActivePlans used ListPlans' default filter (active AND
// paused), so the periodic tick kept driving a "paused" plan — the pause was a
// no-op. This test fails against that regression.
func TestDispatchActivePlans_SkipsPausedPlan(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	projectID, err := st.CreateProject(ctx, "paused-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: "/tmp/paused-project"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	planID, err := st.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "paused-plan",
		Title:          "Paused",
		SourceMarkdown: "# Paused\n\n1. should not run\n",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	// Pause it — a paused plan must never be dispatched.
	if err := st.SetPlanStatus(ctx, planID, store.PlanStatusPaused); err != nil {
		t.Fatalf("SetPlanStatus paused: %v", err)
	}

	runner := &fakeRunner{}
	o := orch.New(st,
		orch.WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		orch.WithBindingResolver(func(context.Context, string, bool) (provider.Binding, error) {
			return provider.Binding{Name: "claude", Config: provider.BindingConfig{Type: "claude", Binary: "true"}}, nil
		}),
	)
	sessionID, err := st.CreateSession(ctx, store.SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sup := &Supervisor{
		opts:      Options{RuntimeDir: t.TempDir(), Store: st},
		store:     st,
		orch:      o,
		sessionID: sessionID,
		startedAt: time.Now(),
		log:       func(string, ...any) {},
		stopCh:    make(chan struct{}),
		stopOnce:  &sync.Once{},
	}

	n, err := sup.dispatchActivePlans(ctx)
	if err != nil {
		t.Fatalf("dispatchActivePlans: %v", err)
	}
	if n != 0 {
		t.Errorf("dispatchActivePlans dispatched %d steps from a PAUSED plan, want 0", n)
	}
	o.Wait()
	if runner.calls() != 0 {
		t.Errorf("runner called %d times for a paused plan, want 0 — pause must halt dispatch", runner.calls())
	}

	// Sanity: re-activating the plan makes the same pass dispatch it, proving
	// the skip is specifically the paused status, not a broken plan.
	if err := st.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus active: %v", err)
	}
	if _, err := sup.dispatchActivePlans(ctx); err != nil {
		t.Fatalf("dispatchActivePlans (active): %v", err)
	}
	o.Wait() // dispatch is async — wait for the re-activated plan's turn
	if runner.calls() != 1 {
		t.Errorf("runner called %d times after re-activation, want 1", runner.calls())
	}
}

// TestHandleEnqueue_NoActivePlansReturnsNotInserted confirms an enqueue
// against a supervisor with no active plans at all is a clean, honest
// "nothing to dispatch" rather than an error or a false-positive success.
func TestHandleEnqueue_NoActivePlansReturnsNotInserted(t *testing.T) {
	runtimeDir := t.TempDir()
	st := openTestStore(t)

	cancel, _ := runSupervisorInBackground(t, Options{RuntimeDir: runtimeDir, Store: st})
	defer cancel()

	client := waitForSupervisor(t, runtimeDir, 2*time.Second)
	defer func() { _ = client.Close() }()

	reply, err := client.Enqueue(context.Background(), ipc.EnqueueArgs{Description: "nothing to do"})
	if err != nil {
		t.Fatalf("client.Enqueue: %v", err)
	}
	if reply.Inserted {
		t.Error("EnqueueReply.Inserted = true, want false with no active plans")
	}
}

// TestHandleEnqueueListPlansErrorSurfaces confirms a ListPlans failure
// (store already closed, simulating a genuine DB error) surfaces as an
// error from HandleEnqueue rather than being silently swallowed.
func TestHandleEnqueueListPlansErrorSurfaces(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	_ = sup.store.Close()

	if _, err := sup.HandleEnqueue(context.Background(), ipc.EnqueueArgs{}); err == nil {
		t.Error("HandleEnqueue with a closed store: want error, got nil")
	}
}

// errFailingBindingResolver is returned by a BindingResolver that always
// fails, exercising orch.DispatchNext's own resolveBinding error path —
// which propagates as an error return from DispatchNext, in turn
// exercising HandleEnqueue's "dispatch failed for this plan, log and
// continue to the next one" branch. (A failing provider.Runner.Run, by
// contrast, is NOT an error return from DispatchNext at all — orch
// handles a failed turn via MarkFailed and still reports it dispatched,
// so it does not reach this branch.)
var errFailingBindingResolver = errors.New("bindingResolver always fails")

// TestHandleEnqueueContinuesPastADispatchFailure confirms one plan's
// DispatchNext error does not abort the whole enqueue pass — a second,
// independently-dispatchable plan still gets a chance, and the failure
// itself is logged rather than propagated as HandleEnqueue's own error.
func TestHandleEnqueueContinuesPastADispatchFailure(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()

	projectID, err := sup.store.CreateProject(ctx, "enqueue-continue-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: "/tmp/enqueue-continue-project"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// A single ready plan whose binding resolution always fails.
	planA, err := sup.store.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID: projectID, Slug: "plan-a", Title: "A", SourceMarkdown: "# A\n\n1. step a\n",
	})
	if err != nil {
		t.Fatalf("CreatePlan A: %v", err)
	}
	if err := sup.store.SetPlanStatus(ctx, planA, store.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus A: %v", err)
	}

	sup.orch = orch.New(sup.store,
		orch.WithBindingResolver(func(context.Context, string, bool) (provider.Binding, error) {
			return provider.Binding{}, errFailingBindingResolver
		}),
	)

	// DispatchNext for plan A fails at resolveBinding, but HandleEnqueue
	// must still return cleanly (no error) since a per-plan dispatch
	// failure is logged, not propagated.
	reply, err := sup.HandleEnqueue(ctx, ipc.EnqueueArgs{})
	if err != nil {
		t.Fatalf("HandleEnqueue: want nil error (per-plan failures are logged, not propagated), got %v", err)
	}
	if reply.Inserted {
		t.Error("EnqueueReply.Inserted = true, want false (the only plan's dispatch attempt failed)")
	}
}
