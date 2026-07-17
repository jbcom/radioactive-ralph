package orch

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// fakeRunner is a test double for provider.Runner that never spawns a real
// agent CLI. It records every Request it was asked to run and returns a
// canned Result/error per call, in order.
type fakeRunner struct {
	// mu guards calls/i: dispatch is asynchronous, so Run is invoked from the
	// dispatched worker goroutines concurrently.
	mu      sync.Mutex
	results []provider.Result
	errs    []error
	calls   []provider.Request
	i       int
}

func (f *fakeRunner) Run(_ context.Context, _ provider.Binding, req provider.Request) (provider.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	idx := f.i
	f.i++
	var res provider.Result
	var err error
	if idx < len(f.results) {
		res = f.results[idx]
	}
	if idx < len(f.errs) {
		err = f.errs[idx]
	}
	f.mu.Unlock()
	return res, err
}

// callReqs returns a snapshot of the recorded requests (lock-safe). Tests read
// this only after o.Wait(), so all dispatched turns have completed.
func (f *fakeRunner) callReqs() []provider.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]provider.Request(nil), f.calls...)
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, store.Options{DSN: store.DSN(filepath.Join(t.TempDir(), "store.db"))})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func mustCreateTestProject(t *testing.T, s *store.Store, name string) string {
	t.Helper()
	// A real directory: the orchestrator now launches workers and runs
	// acceptance re-checks in the project's recorded abs_path checkout (not
	// the process cwd), so a nonexistent path would make exec chdir fail.
	dir := t.TempDir()
	id, err := s.CreateProject(context.Background(), name, []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: dir},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return id
}

func mustCreateTestPlan(t *testing.T, s *store.Store, projectID, slug, title, markdown string) string {
	t.Helper()
	id, err := s.CreatePlan(context.Background(), store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           slug,
		Title:          title,
		SourceMarkdown: markdown,
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	return id
}

const twoStepSequentialPlan = `# Ship the feature

1. write the code
2. write the tests
`

const twoStepParallelPlan = `# Fan out

- task alpha
- task beta
`

const threeStepParallelPlan = `# Fan out wide

- task alpha
- task beta
- task gamma
`

func fakeBindingResolver(name string, nativeFanout bool) BindingResolver {
	return func(_ context.Context, _ string, _ bool) (provider.Binding, error) {
		return provider.Binding{
			Name:   name,
			Config: provider.BindingConfig{Type: name, Binary: "true", NativeFanout: nativeFanout},
		}, nil
	}
}

// TestDispatchNextSequentialDispatchesOnlyFirstStep confirms an ordered
// (sequential) leaf group only dispatches its first not-done step, per
// plan.Decompose's own sequential semantics — the orchestrator must not
// race ahead of dependency order even though DispatchNext is capable of
// dispatching several steps in one call.
func TestDispatchNextSequentialDispatchesOnlyFirstStep(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "seq-project")
	planID := mustCreateTestPlan(t, s, projectID, "seq-plan", "Ship", twoStepSequentialPlan)

	runner := &fakeRunner{
		results: []provider.Result{{AssistantOutput: "did the work"}},
	}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1 (sequential group must gate on its first step)", dispatched)
	}
	o.Wait() // dispatch is async — wait for the provider turn(s) to complete
	calls := runner.callReqs()
	if len(calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(calls))
	}
	got := calls[0].UserPrompt
	if !containsAll(got, "Ship the feature", "write the code") {
		t.Errorf("scoped prompt = %q, want it to mention plan title and step text", got)
	}
	if strings.Contains(got, "write the tests") {
		t.Errorf("scoped prompt leaked the SECOND step's text: %q (context must be scoped to the ready step only)", got)
	}

	tasks, err := s.ListTasks(ctx, planID, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	var doneCount, pendingCount int
	for _, tk := range tasks {
		switch tk.Status {
		case store.TaskStatusDone:
			doneCount++
		case store.TaskStatusPending:
			pendingCount++
		}
	}
	if doneCount != 1 {
		t.Errorf("done tasks = %d, want 1", doneCount)
	}
	if pendingCount != 0 {
		t.Errorf("pending tasks = %d, want 0 (second step not yet materialized until its group is ready)", pendingCount)
	}
}

// panicRunner is a provider.Runner whose turn panics, standing in for a
// misbehaving provider integration or a bug in the turn path.
type panicRunner struct{}

func (panicRunner) Run(context.Context, provider.Binding, provider.Request) (provider.Result, error) {
	panic("boom: simulated provider turn panic")
}

// TestDispatchWorkerPanicIsContained confirms the control invariant that a
// panicking worker turn can never crash the supervisor: the async dispatch
// goroutine recovers, the process survives (o.Wait() returns instead of the
// test binary aborting), the panic is recorded as a worker.dispatch_panic store
// event, and the claimed task is reclaimed to 'pending' immediately (not left
// wedged 'running' until the reaper) so it can be re-dispatched at once.
func TestDispatchWorkerPanicIsContained(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "panic-project")
	planID := mustCreateTestPlan(t, s, projectID, "panic-plan", "Ship", twoStepSequentialPlan)

	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return panicRunner{}, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1", dispatched)
	}
	// If the panic were NOT recovered, the dispatch goroutine would crash the
	// whole test process here rather than letting Wait return.
	o.Wait()

	events, err := s.ListProjectEvents(ctx, projectID, 50)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	var panicEvent *store.Event
	for i := range events {
		if events[i].Kind == "worker.dispatch_panic" {
			panicEvent = &events[i]
			break
		}
	}
	if panicEvent == nil {
		t.Fatalf("no worker.dispatch_panic event recorded; events = %+v", events)
	}
	if !strings.Contains(panicEvent.PayloadJSON, "simulated provider turn panic") {
		t.Errorf("panic event payload = %q, want it to carry the panic value", panicEvent.PayloadJSON)
	}

	// The claimed task must be reclaimed immediately, not stuck 'running' until
	// the 90s reaper window. It returns to 'pending' (no retry penalty for an
	// orchestrator-level failure) and no task should be left 'running'.
	tasks, err := s.ListTasks(ctx, planID, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	var running, pending int
	for _, tk := range tasks {
		switch tk.Status {
		case store.TaskStatusRunning:
			running++
		case store.TaskStatusPending:
			pending++
		}
	}
	if running != 0 {
		t.Errorf("running tasks = %d, want 0 (the panicked task must not be left wedged running)", running)
	}
	if pending != 1 {
		t.Errorf("pending tasks = %d, want 1 (the panicked task reclaimed to pending)", pending)
	}

	// A second dispatch must be able to re-claim the reclaimed task right away.
	redispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("second DispatchNext: %v", err)
	}
	o.Wait()
	if redispatched != 1 {
		t.Errorf("re-dispatched = %d, want 1 (reclaimed task must be immediately dispatchable)", redispatched)
	}
}

// TestDispatchNextParallelDispatchesAllReadySteps confirms an unordered
// (parallel) leaf group dispatches every not-done step together.
func TestDispatchNextParallelDispatchesAllReadySteps(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "par-project")
	planID := mustCreateTestPlan(t, s, projectID, "par-plan", "Fan", twoStepParallelPlan)

	runner := &fakeRunner{
		results: []provider.Result{
			{AssistantOutput: "alpha done"},
			{AssistantOutput: "beta done"},
		},
	}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 2 {
		t.Fatalf("dispatched = %d, want 2 (parallel group dispatches all ready steps)", dispatched)
	}
	o.Wait() // dispatch is async — wait for both provider turns + verification

	progress, err := o.PlanProgress(ctx, planID)
	if err != nil {
		t.Fatalf("PlanProgress: %v", err)
	}
	if progress.Done != 2 || progress.Total != 2 {
		t.Errorf("progress = %+v, want Done=2 Total=2", progress)
	}
}

// TestDispatchNextMaxParallelBoundsDispatch confirms WithMaxParallel caps
// how many steps a single DispatchNext call will dispatch from a parallel
// group.
func TestDispatchNextMaxParallelBoundsDispatch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "cap-project")
	planID := mustCreateTestPlan(t, s, projectID, "cap-plan", "Fan", twoStepParallelPlan)

	runner := &fakeRunner{
		results: []provider.Result{{AssistantOutput: "alpha done"}},
	}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
		WithMaxParallel(1),
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1 (WithMaxParallel(1) caps a 2-step parallel group)", dispatched)
	}
}

// TestDispatchNextNothingReadyReturnsZero confirms a fully-done plan
// dispatches nothing and errors nothing.
func TestDispatchNextNothingReadyReturnsZero(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "done-project")
	planID := mustCreateTestPlan(t, s, projectID, "done-plan", "Ship", twoStepSequentialPlan)

	runner := &fakeRunner{
		results: []provider.Result{{AssistantOutput: "1"}, {AssistantOutput: "2"}},
	}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	// Drain the sequential plan to completion. Dispatch is async, so wait for
	// each dispatched turn to complete (and its task to be marked done) before the
	// next pass computes readiness — otherwise the next step's dependency may not
	// yet be satisfied and the loop would exit early.
	for {
		n, err := o.DispatchNext(ctx, projectID, planID)
		if err != nil {
			t.Fatalf("DispatchNext: %v", err)
		}
		o.Wait()
		if n == 0 {
			break
		}
	}

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext on drained plan: %v", err)
	}
	if dispatched != 0 {
		t.Errorf("dispatched = %d, want 0 on a fully-done plan", dispatched)
	}
}

// TestDispatchNextSpendCapRefusesDispatch is the spend-cap test: a
// provider already at/over its configured cap must not be dispatched to,
// even though its step is otherwise ready.
func TestDispatchNextSpendCapRefusesDispatch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "spend-project")
	planID := mustCreateTestPlan(t, s, projectID, "spend-plan", "Ship", twoStepSequentialPlan)

	// Pre-seed spend for "claude" already at the cap.
	if err := s.RecordSpend(ctx, store.RecordSpendOpts{
		ProjectID: projectID, Provider: "claude", CostUSD: 5.00,
	}); err != nil {
		t.Fatalf("RecordSpend: %v", err)
	}

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "should not run"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
		WithSpendCap("claude", 5.00),
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 0 {
		t.Errorf("dispatched = %d, want 0 (provider is at its spend cap)", dispatched)
	}
	o.Wait()
	if calls := runner.callReqs(); len(calls) != 0 {
		t.Errorf("runner was called %d times, want 0 — spend cap must refuse BEFORE dispatch", len(calls))
	}

	events, err := s.ListProjectEvents(ctx, projectID, 10)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind == "worker.admission_refused" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a worker.admission_refused event, got %+v", events)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
