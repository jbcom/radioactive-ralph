package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jonboulle/clockwork"
)

// newTestSupervisor builds a *Supervisor directly (white-box: this file is
// package supervisor, not supervisor_test) with a real session row and a
// store built on the given clock, WITHOUT going through Run/Acquire —
// exercising tick/HandleReloadConfig/HandleAttach/HandleStatus does not
// need a bound socket or PID lock.
func newTestSupervisor(t *testing.T, clock clockwork.Clock) *Supervisor {
	t.Helper()
	dbPath := t.TempDir() + "/store.db"
	st, err := store.Open(context.Background(), store.Options{DSN: store.DSN(dbPath), Clock: clock})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	sessionID, err := st.CreateSession(context.Background(), store.SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	return &Supervisor{
		opts:      Options{RuntimeDir: t.TempDir(), Store: st},
		store:     st,
		orch:      orch.New(st),
		sessionID: sessionID,
		startedAt: time.Now(),
		log:       func(string, ...any) {},
		stopCh:    make(chan struct{}),
		stopOnce:  &sync.Once{},
	}
}

// TestTickReclaimsStaleAndHeartbeatsSession drives Supervisor.tick
// directly (white-box) rather than waiting out the real 15s
// reaperInterval: it seeds a task claimed by a worker whose heartbeat is
// already stale beyond staleAfter, calls tick once, and confirms both
// halves of what tick does — the store-level reaper reclaim runs (the
// task returns to pending) AND the supervisor's own session heartbeat is
// refreshed (so peers/reapers never mistake a live supervisor for stale).
func TestTickReclaimsStaleAndHeartbeatsSession(t *testing.T) {
	clock := clockwork.NewFakeClockAt(time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC))
	sup := newTestSupervisor(t, clock)
	ctx := context.Background()

	projectID, err := sup.store.CreateProject(ctx, "tick-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: "/tmp/tick-project"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	planID, err := sup.store.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID: projectID, Slug: "tick-plan", Title: "Tick", SourceMarkdown: "# Tick\n\n1. step\n",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := sup.store.CreateTask(ctx, store.CreateTaskOpts{PlanID: planID, ID: "a", Description: "first"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	workerSessionID, err := sup.store.CreateSession(ctx, store.SessionOpts{Role: "worker", PID: 2, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession (worker): %v", err)
	}
	workerID, err := sup.store.CreateWorker(ctx, store.WorkerOpts{
		SessionID: workerSessionID, Provider: "claude", SubprocessPID: 100, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if _, err := sup.store.ClaimNextReady(ctx, planID, workerSessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	// Advance well past staleAfter (90s) with no further worker heartbeat.
	clock.Advance(staleAfter * 3)

	var beforeHeartbeat string
	if err := sup.store.DB().QueryRowContext(ctx, "SELECT last_heartbeat FROM sessions WHERE id = ?", sup.sessionID).Scan(&beforeHeartbeat); err != nil {
		t.Fatalf("read supervisor session heartbeat before tick: %v", err)
	}

	sup.tick(ctx)

	task, err := sup.store.GetTask(ctx, planID, "a")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != store.TaskStatusPending {
		t.Errorf("task status after tick = %q, want pending (reclaimed)", task.Status)
	}

	var afterHeartbeat string
	if err := sup.store.DB().QueryRowContext(ctx, "SELECT last_heartbeat FROM sessions WHERE id = ?", sup.sessionID).Scan(&afterHeartbeat); err != nil {
		t.Fatalf("read supervisor session heartbeat after tick: %v", err)
	}
	if afterHeartbeat == beforeHeartbeat {
		t.Error("supervisor's own session heartbeat did not change after tick")
	}
}

// TestHandleReloadConfigIsANoOp confirms the current no-op behavior stays
// intact and errorless — HandleReloadConfig is a documented placeholder
// (config reload semantics belong to vconfig, not this minimal
// supervisor), so this pins the observable contract rather than the
// internal implementation.
func TestHandleReloadConfigIsANoOp(t *testing.T) {
	sup := newTestSupervisor(t, clockwork.NewRealClock())
	if err := sup.HandleReloadConfig(context.Background()); err != nil {
		t.Errorf("HandleReloadConfig: want nil, got %v", err)
	}
}

// TestHandleAttachRequiresProjectID confirms an attach with no project scope is
// rejected rather than streaming another project's (or every project's) events.
func TestHandleAttachRequiresProjectID(t *testing.T) {
	sup := newTestSupervisor(t, clockwork.NewRealClock())
	err := sup.HandleAttach(context.Background(), ipc.AttachArgs{}, func(_ json.RawMessage) error { return nil })
	if err == nil {
		t.Error("HandleAttach with empty ProjectID: want error, got nil")
	}
}

// TestHandleAttachBlocksOnEmptyProjectUntilCancelled confirms that with no
// matching events the stream stays open (a quiet, still-live feed) and ends
// cleanly when ctx is cancelled — the client never sees an error or a premature
// close.
func TestHandleAttachBlocksOnEmptyProjectUntilCancelled(t *testing.T) {
	ctx := context.Background()
	sup := newTestSupervisor(t, clockwork.NewRealClock())
	projectID, err := sup.store.CreateProject(ctx, "attach-empty", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = sup.HandleAttach(runCtx, ipc.AttachArgs{ProjectID: projectID}, func(_ json.RawMessage) error {
		t.Error("no events exist; emit must not be called")
		return nil
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("HandleAttach: want nil once ctx is done, got %v", err)
	}
	if elapsed < 250*time.Millisecond {
		t.Errorf("HandleAttach returned after %s, want it to block until ctx was done (~300ms)", elapsed)
	}
}

// TestHandleAttachEmitsNewEvents confirms the tail delivers events that land
// AFTER the attach begins, in id order, and that AfterID=0 also delivers a
// pre-existing event (the client owns the cursor; 0 means from the beginning).
func TestHandleAttachEmitsNewEvents(t *testing.T) {
	ctx := context.Background()
	sup := newTestSupervisor(t, clockwork.NewRealClock())
	projectID, err := sup.store.CreateProject(ctx, "attach-emits", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// One event exists before the attach; with AfterID=0 it must be delivered.
	if err := sup.store.Emit(ctx, store.EmitOpts{ProjectID: projectID, Kind: "project.created"}); err != nil {
		t.Fatalf("Emit pre-existing: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	got := make(chan ipc.AttachEvent, 8)
	done := make(chan error, 1)
	go func() {
		done <- sup.HandleAttach(runCtx, ipc.AttachArgs{ProjectID: projectID}, func(raw json.RawMessage) error {
			var ev ipc.AttachEvent
			if err := json.Unmarshal(raw, &ev); err != nil {
				return err
			}
			got <- ev
			return nil
		})
	}()

	// First: the pre-existing event.
	select {
	case ev := <-got:
		if ev.Kind != "project.created" {
			t.Errorf("first event Kind = %q, want project.created", ev.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the pre-existing event")
	}

	// Now emit a new one; the live tail must pick it up.
	if err := sup.store.Emit(ctx, store.EmitOpts{ProjectID: projectID, Kind: "project.touched"}); err != nil {
		t.Fatalf("Emit new: %v", err)
	}
	select {
	case ev := <-got:
		if ev.Kind != "project.touched" {
			t.Errorf("second event Kind = %q, want project.touched", ev.Kind)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the newly-emitted event")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("HandleAttach returned %v, want nil on ctx cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HandleAttach did not return after ctx cancel")
	}
}

// TestHandleAttachPermanentStoreErrorEndsStream confirms a permanent store
// failure (here: a closed DB) ends the stream with an error rather than
// spinning forever, logging every tick, and leaving the client attached to a
// permanently stale feed.
func TestHandleAttachPermanentStoreErrorEndsStream(t *testing.T) {
	ctx := context.Background()
	sup := newTestSupervisor(t, clockwork.NewRealClock())
	projectID, err := sup.store.CreateProject(ctx, "attach-permerr", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// Close the store so EventsAfter returns a permanent (connection-done) error.
	if err := sup.store.Close(); err != nil {
		t.Fatalf("Close store: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- sup.HandleAttach(ctx, ipc.AttachArgs{ProjectID: projectID}, func(_ json.RawMessage) error { return nil })
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("HandleAttach: want an error when the store is permanently broken, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("HandleAttach spun instead of ending on a permanent store error")
	}
}

// TestHandleAttachEmitStopEndsStream confirms that when emit reports the client
// is gone (returns an error), the tail ends cleanly with a nil error — the
// documented emit contract the IPC server relies on.
func TestHandleAttachEmitStopEndsStream(t *testing.T) {
	ctx := context.Background()
	sup := newTestSupervisor(t, clockwork.NewRealClock())
	projectID, err := sup.store.CreateProject(ctx, "attach-emitstop", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := sup.store.Emit(ctx, store.EmitOpts{ProjectID: projectID, Kind: "project.created"}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	err = sup.HandleAttach(ctx, ipc.AttachArgs{ProjectID: projectID}, func(_ json.RawMessage) error {
		return fmt.Errorf("client gone")
	})
	if err != nil {
		t.Errorf("HandleAttach: want nil when emit ends the stream, got %v", err)
	}
}

// TestHandleStatusDegradesOnCountError confirms a ListRunningWorkers failure
// degrades HandleStatus's ActiveWorkers to 0 (and Workers to empty) rather than
// failing the whole status reply — closing the store out from under it is the
// simplest way to force that query to fail deterministically.
func TestHandleStatusDegradesOnCountError(t *testing.T) {
	sup := newTestSupervisor(t, clockwork.NewRealClock())
	_ = sup.store.Close()

	status, err := sup.HandleStatus(context.Background())
	if err != nil {
		t.Fatalf("HandleStatus: want nil error even when the worker query fails, got %v", err)
	}
	if status.ActiveWorkers != 0 {
		t.Errorf("ActiveWorkers = %d, want 0 (degraded on query failure)", status.ActiveWorkers)
	}
	if len(status.Workers) != 0 {
		t.Errorf("Workers = %v, want empty (degraded on query failure)", status.Workers)
	}
	if status.PID == 0 {
		t.Error("PID = 0, want the real process pid regardless of the query failure")
	}
}

// TestHandleStatusPopulatesWorkers confirms HandleStatus exposes per-worker
// detail — the store worker-row id plus its plan/task — so a client (the GUI)
// can name a specific worker to kill. Before this the Workers slice was always
// nil, leaving the kill affordance dead.
func TestHandleStatusPopulatesWorkers(t *testing.T) {
	sup := newTestSupervisor(t, clockwork.NewRealClock())
	ctx := context.Background()

	projectID, err := sup.store.CreateProject(ctx, "sw-proj", []store.Fingerprint{{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()}})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	planID, err := sup.store.CreatePlan(ctx, store.CreatePlanOpts{ProjectID: projectID, Slug: "sw", Title: "SW", SourceMarkdown: "# SW\n\n1. step\n"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := sup.store.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus active: %v", err)
	}
	sessionID, err := sup.store.CreateSession(ctx, store.SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := sup.store.CreateWorker(ctx, store.WorkerOpts{SessionID: sessionID, Provider: "claude", SubprocessPID: 1, SubprocessStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if err := sup.store.CreateTask(ctx, store.CreateTaskOpts{PlanID: planID, ID: "t", Description: "d"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := sup.store.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if err := sup.store.SetWorkerTask(ctx, workerID, planID, "t"); err != nil {
		t.Fatalf("SetWorkerTask: %v", err)
	}

	status, err := sup.HandleStatus(ctx)
	if err != nil {
		t.Fatalf("HandleStatus: %v", err)
	}
	if status.ActiveWorkers != 1 {
		t.Fatalf("ActiveWorkers = %d, want 1", status.ActiveWorkers)
	}
	if len(status.Workers) != 1 {
		t.Fatalf("Workers = %d, want 1", len(status.Workers))
	}
	w := status.Workers[0]
	if w.WorkerID != workerID || w.PlanID != planID || w.TaskID != "t" {
		t.Errorf("worker summary = %+v, want id=%s plan=%s task=t (WorkerID is the kill key)", w, workerID, planID)
	}

	// The plan/task counters must be populated too (they were always zero before
	// StatusCounts was wired in): the plan is active and its one task is running.
	if status.ActivePlans != 1 {
		t.Errorf("ActivePlans = %d, want 1", status.ActivePlans)
	}
	if status.RunningTasks != 1 {
		t.Errorf("RunningTasks = %d, want 1 (the claimed task)", status.RunningTasks)
	}
}

func TestHostname(t *testing.T) {
	// hostname() should never panic and, on any sane CI/dev machine,
	// return a non-empty string.
	if h := hostname(); h == "" {
		t.Log("hostname() returned empty — acceptable per its documented fallback, but worth knowing in CI logs")
	}
}

// tickFakeRunner is a canned successful provider.Runner for tick-dispatch
// tests: it records call count and returns non-empty output so the
// orchestrator's judgment-only acceptance fallback accepts the step.
type tickFakeRunner struct {
	// mu guards nCall: dispatch is async, so Run is invoked from the dispatched
	// worker goroutine concurrently with the test's assertions.
	mu    sync.Mutex
	nCall int
}

func (f *tickFakeRunner) Run(context.Context, provider.Binding, provider.Request) (provider.Result, error) {
	f.mu.Lock()
	f.nCall++
	f.mu.Unlock()
	return provider.Result{AssistantOutput: "did the work"}, nil
}

func (f *tickFakeRunner) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.nCall
}

// TestTickDrivesDispatchForActivePlan proves the periodic tick — not just an
// explicit HandleEnqueue — advances an active plan. Without the dispatch
// pass in tick(), a seeded active plan would sit forever unless a client
// happened to call Enqueue. Here a single tick() must dispatch the plan's
// one ready step and drive it to done.
func TestTickDrivesDispatchForActivePlan(t *testing.T) {
	sup := newTestSupervisor(t, nil)
	ctx := context.Background()

	runner := &tickFakeRunner{}
	sup.orch = orch.New(sup.store,
		orch.WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		orch.WithBindingResolver(func(context.Context, string, bool) (provider.Binding, error) {
			return provider.Binding{Name: "claude", Config: provider.BindingConfig{Type: "claude", Binary: "true"}}, nil
		}),
	)

	projectID, err := sup.store.CreateProject(ctx, "tick-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	planID, err := sup.store.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "tick-plan",
		Title:          "Ship",
		SourceMarkdown: "# Ship\n\n1. write the code\n",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := sup.store.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}

	sup.tick(ctx)
	sup.orch.Wait() // dispatch is async — wait for the tick's dispatched turn

	if runner.calls() != 1 {
		t.Fatalf("runner.calls = %d, want 1 — tick must dispatch the active plan's ready step", runner.calls())
	}
	task, err := sup.store.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != store.TaskStatusDone {
		t.Errorf("task status = %q, want done after the tick-triggered dispatch", task.Status)
	}
}
