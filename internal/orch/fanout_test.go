package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

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
	if len(runner.calls) != 1 {
		t.Fatalf("runner called %d times, want EXACTLY 1 — a NativeFanout provider must get one dispatch for the whole group", len(runner.calls))
	}

	prompt := runner.calls[0].UserPrompt
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
	if len(runner.calls) != 3 {
		t.Fatalf("runner called %d times, want EXACTLY 3 — a non-fanout provider must get one dispatch per step", len(runner.calls))
	}

	progress, err := o.PlanProgress(ctx, planID)
	if err != nil {
		t.Fatalf("PlanProgress: %v", err)
	}
	if progress.Done != 3 || progress.Total != 3 {
		t.Errorf("progress = %+v, want Done=3 Total=3", progress)
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
	if len(runner.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(runner.calls))
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
