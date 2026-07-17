package orch

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// fakeRunner is a test double for provider.Runner that never spawns a real
// agent CLI. It records every Request it was asked to run and returns a
// canned Result/error per call, in order.
type fakeRunner struct {
	results []provider.Result
	errs    []error
	calls   []provider.Request
	i       int
}

func (f *fakeRunner) Run(_ context.Context, _ provider.Binding, req provider.Request) (provider.Result, error) {
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
	return res, err
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
	if len(runner.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(runner.calls))
	}
	got := runner.calls[0].UserPrompt
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

	// Drain the sequential plan to completion.
	for {
		n, err := o.DispatchNext(ctx, projectID, planID)
		if err != nil {
			t.Fatalf("DispatchNext: %v", err)
		}
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
	if len(runner.calls) != 0 {
		t.Errorf("runner was called %d times, want 0 — spend cap must refuse BEFORE dispatch", len(runner.calls))
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
