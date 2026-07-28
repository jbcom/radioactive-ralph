package orch

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

type bindingRecordingRunner struct {
	mu       sync.Mutex
	bindings []string
}

type purposeAwarePoolResolver struct {
	mu         sync.Mutex
	names      []string
	next       map[string]int
	probes     map[string]int
	dispatches map[string]int
}

func newPurposeAwarePoolResolver(names ...string) *purposeAwarePoolResolver {
	return &purposeAwarePoolResolver{
		names:      names,
		next:       make(map[string]int),
		probes:     make(map[string]int),
		dispatches: make(map[string]int),
	}
}

func (r *purposeAwarePoolResolver) resolve(_ context.Context, projectID string, _ bool, purpose BindingResolutionPurpose) (provider.Binding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := r.names[r.next[projectID]%len(r.names)]
	if purpose == BindingDispatch {
		r.next[projectID]++
		r.dispatches[projectID]++
	} else {
		r.probes[projectID]++
	}
	return provider.Binding{
		Name:   name,
		Config: provider.BindingConfig{Type: name, Binary: "true", NativeFanout: false},
	}, nil
}

func (r *purposeAwarePoolResolver) counts(projectID string) (probes, dispatches int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.probes[projectID], r.dispatches[projectID]
}

type bindingBlockingRunner struct {
	started chan string
}

func (r *bindingBlockingRunner) Run(ctx context.Context, binding provider.Binding, _ provider.Request) (provider.Result, error) {
	r.started <- binding.Name
	<-ctx.Done()
	return provider.Result{}, ctx.Err()
}

func (r *bindingRecordingRunner) Run(_ context.Context, binding provider.Binding, _ provider.Request) (provider.Result, error) {
	r.mu.Lock()
	r.bindings = append(r.bindings, binding.Name)
	r.mu.Unlock()
	return provider.Result{AssistantOutput: "done"}, nil
}

func (r *bindingRecordingRunner) names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.bindings...)
}

// TestDispatchNextNativeFanoutDelegatesWholeGroupToOneWorker is the proof
// for the fan-out delegation implementation: a 3-step PARALLEL group whose
// resolved binding has NativeFanout=true must be dispatched as exactly ONE
// worker turn (one call to Runner.Run) whose prompt mentions every step in
// the group, and that single turn's evidence must independently complete
// EVERY task in the group — not just the first one.
func TestDispatchNextNativeFanoutDelegatesWholeGroupToOneWorker(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "fanout-project")
	planID := mustCreateTestPlan(t, s, projectID, "fanout-plan", "Fan", threeStepParallelPlan)

	runner := &fakeRunner{
		results: []provider.Result{{AssistantOutput: "all three done"}},
	}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", true)), // NativeFanout=true
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 3 {
		t.Fatalf("dispatched = %d, want 3 (all steps in the group count as dispatched even though delegated to one worker)", dispatched)
	}
	o.Wait() // dispatch is async — wait for the fan-out turn to complete
	calls := runner.callReqs()
	if len(calls) != 1 {
		t.Fatalf("runner called %d times, want EXACTLY 1 — a NativeFanout provider must get one dispatch for the whole group", len(calls))
	}

	prompt := calls[0].UserPrompt
	if !containsAll(prompt, "task alpha", "task beta", "task gamma") {
		t.Errorf("fan-out prompt = %q, want it to mention all three steps", prompt)
	}

	progress, err := o.PlanProgress(ctx, planID)
	if err != nil {
		t.Fatalf("PlanProgress: %v", err)
	}
	if progress.Done != 3 || progress.Total != 3 {
		t.Errorf("progress = %+v, want Done=3 Total=3 (one worker's evidence must complete every task in the group)", progress)
	}
}

// TestDispatchNextNonFanoutProviderDispatchesOneWorkerPerStep is the
// control case for the same 3-step parallel group: a provider WITHOUT
// NativeFanout must still get one Ralph-managed worker dispatch PER step,
// exactly as before this feature existed.
func TestDispatchNextNonFanoutProviderDispatchesOneWorkerPerStep(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "nonfanout-project")
	planID := mustCreateTestPlan(t, s, projectID, "nonfanout-plan", "Fan", threeStepParallelPlan)

	runner := &fakeRunner{
		results: []provider.Result{
			{AssistantOutput: "alpha done"},
			{AssistantOutput: "beta done"},
			{AssistantOutput: "gamma done"},
		},
	}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)), // NativeFanout=false
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 3 {
		t.Fatalf("dispatched = %d, want 3", dispatched)
	}
	o.Wait() // dispatch is async — wait for all three per-step turns to complete
	if calls := runner.callReqs(); len(calls) != 3 {
		t.Fatalf("runner called %d times, want EXACTLY 3 — a non-fanout provider must get one dispatch per step", len(calls))
	}

	progress, err := o.PlanProgress(ctx, planID)
	if err != nil {
		t.Fatalf("PlanProgress: %v", err)
	}
	if progress.Done != 3 || progress.Total != 3 {
		t.Errorf("progress = %+v, want Done=3 Total=3", progress)
	}
}

// TestDispatchNextRalphManagedPoolDoesNotSkipProbeBinding proves the native
// fan-out capability probe peeks without consuming the pool cursor. Each
// admitted worker gets one dispatch resolution, and all pool members remain
// eligible.
func TestDispatchNextRalphManagedPoolDoesNotSkipProbeBinding(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "pool-project")
	planID := mustCreateTestPlan(t, s, projectID, "pool-plan", "Pool", threeStepParallelPlan)

	runner := &bindingRecordingRunner{}
	names := []string{"claude", "codex", "opencode"}
	pool := newPurposeAwarePoolResolver(names...)

	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(pool.resolve),
	)
	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 3 {
		t.Fatalf("dispatched = %d, want 3", dispatched)
	}
	o.Wait()

	probes, dispatches := pool.counts(projectID)
	if probes != 1 || dispatches != 3 {
		t.Fatalf("binding resolver calls = probes:%d dispatches:%d, want probes:1 dispatches:3", probes, dispatches)
	}

	got := runner.names()
	counts := map[string]int{}
	for _, name := range got {
		counts[name]++
	}
	for _, name := range names {
		if counts[name] != 1 {
			t.Errorf("provider %q worker count = %d, want 1; all bindings = %v", name, counts[name], got)
		}
	}
}

func TestDispatchNextSaturatedProbeDoesNotConsumePoolCursor(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	fillerProjectID := mustCreateTestProject(t, s, "pool-saturation-fill")
	fillerPlanA := mustCreateTestPlan(t, s, fillerProjectID, "fill-a", "Fill A", twoStepSequentialPlan)
	fillerPlanB := mustCreateTestPlan(t, s, fillerProjectID, "fill-b", "Fill B", twoStepSequentialPlan)
	projectID := mustCreateTestProject(t, s, "pool-saturation-target")
	planID := mustCreateTestPlan(t, s, projectID, "pool-saturation-plan", "Pool", threeStepParallelPlan)

	runner := &bindingBlockingRunner{started: make(chan string, 8)}
	pool := newPurposeAwarePoolResolver("claude", "codex")
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(pool.resolve),
		WithMaxParallel(2),
	)

	for _, fillerPlanID := range []string{fillerPlanA, fillerPlanB} {
		n, err := o.DispatchNext(ctx, fillerProjectID, fillerPlanID)
		if err != nil {
			t.Fatalf("dispatch filler: %v", err)
		}
		if n != 1 {
			t.Fatalf("filler dispatched = %d, want 1", n)
		}
	}
	for range 2 {
		select {
		case <-runner.started:
		case <-time.After(3 * time.Second):
			t.Fatal("filler runner did not start")
		}
	}

	n, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext while saturated: %v", err)
	}
	if n != 0 {
		t.Fatalf("saturated dispatch = %d, want 0", n)
	}
	probes, dispatches := pool.counts(projectID)
	if probes != 1 || dispatches != 0 {
		t.Fatalf("saturated resolver calls = probes:%d dispatches:%d, want probes:1 dispatches:0", probes, dispatches)
	}

	workers := runningWorkerIDs(o)
	if len(workers) != 2 {
		t.Fatalf("running filler workers = %d, want 2", len(workers))
	}
	o.KillWorker(workers[0])

	deadline := time.Now().Add(3 * time.Second)
	for n == 0 && time.Now().Before(deadline) {
		n, err = o.DispatchNext(ctx, projectID, planID)
		if err != nil {
			t.Fatalf("DispatchNext after capacity released: %v", err)
		}
		if n == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if n != 1 {
		t.Fatalf("dispatch after capacity released = %d, want 1", n)
	}
	select {
	case name := <-runner.started:
		if name != "claude" {
			t.Fatalf("first admitted target binding = %q, want claude; saturated probes must not advance", name)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("target runner did not start")
	}

	for _, workerID := range runningWorkerIDs(o) {
		o.KillWorker(workerID)
	}
	o.Wait()
}

const twoStepParallelApprovalPlan = `# Guarded pool

- task alpha [approval]
- task beta [approval]
`

func TestDispatchNextApprovalProbeDoesNotConsumePoolCursor(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "pool-approval-project")
	planID := mustCreateTestPlan(t, s, projectID, "pool-approval-plan", "Pool", twoStepParallelApprovalPlan)

	runner := &bindingRecordingRunner{}
	pool := newPurposeAwarePoolResolver("claude", "codex")
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(pool.resolve),
	)

	n, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext while gated: %v", err)
	}
	if n != 0 {
		t.Fatalf("gated dispatch = %d, want 0", n)
	}
	// A gated pass must never CONSUME the pool cursor. It used to still probe,
	// because readiness came from the markdown and the approval gate was checked
	// per-step afterwards. Now the store's edge walk excludes an
	// approval-gated task from the ready set outright, so a fully gated plan
	// yields no partitions and dispatch returns before resolving anything —
	// strictly better, since probing a binding for work that cannot run is
	// wasted. Either way zero dispatches is the invariant, and the assertion
	// after approval below is what proves the cursor did not move.
	_, dispatches := pool.counts(projectID)
	if dispatches != 0 {
		t.Fatalf("gated resolver dispatches = %d, want 0", dispatches)
	}

	if found, changed, err := s.ApproveTask(ctx, planID, "0.0"); err != nil || !found || !changed {
		t.Fatalf("ApproveTask = (found=%v changed=%v err=%v), want (true,true,nil)", found, changed, err)
	}
	n, err = o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext after approval: %v", err)
	}
	if n != 1 {
		t.Fatalf("approved dispatch = %d, want 1", n)
	}
	o.Wait()
	got := runner.names()
	if len(got) != 1 || got[0] != "claude" {
		t.Fatalf("admitted bindings = %v, want [claude]; gated probes must not advance", got)
	}
}

// TestDispatchNextNativeFanoutRunnerErrorFailsEveryTaskInGroup confirms the
// error path maps back onto every claimed task, not just the first.
func TestDispatchNextNativeFanoutRunnerErrorFailsEveryTaskInGroup(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "fanout-err-project")
	planID := mustCreateTestPlan(t, s, projectID, "fanout-err-plan", "Fan", threeStepParallelPlan)

	runner := &fakeRunner{errs: []error{errRunnerBoom}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", true)),
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 3 {
		t.Fatalf("dispatched = %d, want 3", dispatched)
	}
	o.Wait() // dispatch is async — wait for the fan-out turn (and its per-task fails)
	if calls := runner.callReqs(); len(calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(calls))
	}

	tasks, err := s.ListTasks(ctx, planID, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	for _, tk := range tasks {
		if tk.Status == store.TaskStatusDone {
			t.Errorf("task %s marked done despite the provider Run erroring", tk.ID)
		}
	}
}

// TestDispatchNextNativeFanoutOnlyAppliesToParallelGroups confirms a
// sequential group is NOT eligible for fan-out delegation even when the
// resolved binding has NativeFanout=true — only one step is ever ready in
// a sequential group, so there is no "whole group" to delegate.
func TestDispatchNextNativeFanoutOnlyAppliesToParallelGroups(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "fanout-seq-project")
	planID := mustCreateTestPlan(t, s, projectID, "fanout-seq-plan", "Ship", twoStepSequentialPlan)

	runner := &fakeRunner{
		results: []provider.Result{{AssistantOutput: "did the work"}},
	}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", true)),
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1 (sequential group must gate on its first step regardless of NativeFanout)", dispatched)
	}
}
