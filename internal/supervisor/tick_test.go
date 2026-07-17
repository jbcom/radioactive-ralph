package supervisor

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

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

// TestHandleAttachBlocksUntilContextCancelled confirms HandleAttach's
// documented behavior: no events yet, but it blocks (rather than
// returning immediately) until ctx is cancelled, and then returns nil
// (the durable event/attach surface is a later phase — a connected
// client sees a quiet, cleanly-ended stream, not an error).
func TestHandleAttachBlocksUntilContextCancelled(t *testing.T) {
	sup := newTestSupervisor(t, clockwork.NewRealClock())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := sup.HandleAttach(ctx, func(_ json.RawMessage) error { return nil })
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("HandleAttach: want nil once ctx is done, got %v", err)
	}
	if elapsed < 90*time.Millisecond {
		t.Errorf("HandleAttach returned after %s, want it to block until ctx was done (~100ms)", elapsed)
	}
}

// TestHandleStatusDegradesOnCountError confirms a CountRunningWorkers
// failure degrades HandleStatus's ActiveWorkers to 0 rather than failing
// the whole status reply — closing the store out from under it is the
// simplest way to force that query to fail deterministically.
func TestHandleStatusDegradesOnCountError(t *testing.T) {
	sup := newTestSupervisor(t, clockwork.NewRealClock())
	_ = sup.store.Close()

	status, err := sup.HandleStatus(context.Background())
	if err != nil {
		t.Fatalf("HandleStatus: want nil error even when the count query fails, got %v", err)
	}
	if status.ActiveWorkers != 0 {
		t.Errorf("ActiveWorkers = %d, want 0 (degraded on count failure)", status.ActiveWorkers)
	}
	if status.PID == 0 {
		t.Error("PID = 0, want the real process pid regardless of the count failure")
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
type tickFakeRunner struct{ calls int }

func (f *tickFakeRunner) Run(context.Context, provider.Binding, provider.Request) (provider.Result, error) {
	f.calls++
	return provider.Result{AssistantOutput: "did the work"}, nil
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

	if runner.calls != 1 {
		t.Fatalf("runner.calls = %d, want 1 — tick must dispatch the active plan's ready step", runner.calls)
	}
	task, err := sup.store.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != store.TaskStatusDone {
		t.Errorf("task status = %q, want done after the tick-triggered dispatch", task.Status)
	}
}
