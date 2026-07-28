package orch

import (
	"context"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// requiresFanoutPlan annotates its single step with a `requires` list. The
// grammar already parses and persists this field; these tests are the proof
// that dispatch now ENFORCES it.
const requiresFanoutPlan = "# Cap group\n\n" +
	"- delegate the whole group\n\n" +
	"   ```ralph-task\n   {\"id\": \"needs-fanout\", \"requires\": [\"native_fanout\"]}\n   ```\n"

const requiresUnknownCapabilityPlan = "# Cap group\n\n" +
	"- typo in the requires list\n\n" +
	"   ```ralph-task\n   {\"id\": \"typo\", \"requires\": [\"nativefanout\"]}\n   ```\n"

const requiresSatisfiedPlan = "# Cap group\n\n" +
	"- a task whose requirement the binding meets\n\n" +
	"   ```ralph-task\n   {\"id\": \"ok\", \"requires\": [\"native_fanout\"]}\n   ```\n"

// TestDispatchBlocksTaskWhoseRequirementTheBindingCannotMeet is the point of
// the whole field. Before this, `requires` parsed and persisted and NOTHING
// read it, so a task declaring it needs native fan-out ran happily on a binding
// that cannot fan out — the exact silent-wrong-provider outcome the annotation
// exists to prevent.
func TestDispatchBlocksTaskWhoseRequirementTheBindingCannotMeet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "cap-block")
	planID := mustCreateTestPlan(t, s, projectID, "cap-block", "Cap", requiresFanoutPlan)

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "should never run"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)), // no native fan-out
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if dispatched != 0 {
		t.Fatalf("dispatched = %d, want 0 — a task requiring a capability the "+
			"binding lacks must not run", dispatched)
	}
	if calls := runner.callReqs(); len(calls) != 0 {
		t.Fatalf("runner called %d times; the provider must never see a task it "+
			"cannot satisfy", len(calls))
	}

	// Blocked VISIBLY, not silently skipped: an operator polling status has to
	// be able to tell "waiting on capacity" from "will never run here".
	task, err := s.GetTask(ctx, planID, "needs-fanout")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "needs-fanout")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if task.Status != store.TaskStatusBlockedCapability {
		t.Fatalf("status = %q, want %q — a silently skipped task looks identical "+
			"to one waiting on capacity and stalls the plan with no explanation",
			task.Status, store.TaskStatusBlockedCapability)
	}
	if !strings.Contains(meta.BlockedReason, "native_fanout") {
		t.Errorf("BlockedReason = %q, want it to name the missing capability",
			meta.BlockedReason)
	}
}

// TestDispatchBlocksTaskWithAnUnknownRequirement fails closed on a typo rather
// than dispatching. A key outside the closed vocabulary can never be satisfied,
// so admitting it would run the task on a provider chosen by accident.
func TestDispatchBlocksTaskWithAnUnknownRequirement(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "cap-typo")
	planID := mustCreateTestPlan(t, s, projectID, "cap-typo", "Cap", requiresUnknownCapabilityPlan)

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "should never run"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", true)), // fully capable
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if dispatched != 0 {
		t.Fatalf("dispatched = %d, want 0 — an unrecognized capability key is a "+
			"plan defect and must not dispatch even against a capable binding", dispatched)
	}
	task, err := s.GetTask(ctx, planID, "typo")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "typo")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if task.Status != store.TaskStatusBlockedCapability {
		t.Fatalf("status = %q, want %q", task.Status, store.TaskStatusBlockedCapability)
	}
	if !strings.Contains(meta.BlockedReason, "nativefanout") {
		t.Errorf("BlockedReason = %q, want it to quote the unrecognized key so the "+
			"operator can find the typo", meta.BlockedReason)
	}
}

// TestDispatchRunsTaskWhoseRequirementIsSatisfied is the control. A gate that
// blocked satisfiable tasks would be worse than no gate at all.
func TestDispatchRunsTaskWhoseRequirementIsSatisfied(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "cap-ok")
	planID := mustCreateTestPlan(t, s, projectID, "cap-ok", "Cap", requiresSatisfiedPlan)

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "done"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", true)), // native fan-out present
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1 — the binding satisfies the requirement", dispatched)
	}
	if calls := runner.callReqs(); len(calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(calls))
	}
}

// TestDispatchIsUnaffectedByAnEmptyRequiresList is the compatibility guard:
// every plan written before this gate existed declares no requirements, and
// none of them may change behavior.
func TestDispatchIsUnaffectedByAnEmptyRequiresList(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "cap-none")
	planID := mustCreateTestPlan(t, s, projectID, "cap-none", "Cap", threeStepParallelPlan)

	runner := &fakeRunner{results: []provider.Result{
		{AssistantOutput: "alpha"}, {AssistantOutput: "beta"}, {AssistantOutput: "gamma"},
	}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()
	if dispatched != 3 {
		t.Fatalf("dispatched = %d, want 3 — unannotated plans must be untouched "+
			"by the capability gate", dispatched)
	}
}

// TestFixingTheBindingUnblocksACapabilityBlockedTask is CodeRabbit's P1 on
// #247, and it is the difference between a gate and a trap.
//
// A blocked task keeps status blocked_capability, and ClaimTask accepts only
// pending or ready. So once an operator FIXES the configuration — the entire
// remedy the block exists to prompt — the task stayed permanently unclaimable
// and the plan never completed. Blocking must be reversible by the action it
// asks for.
func TestFixingTheBindingUnblocksACapabilityBlockedTask(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "cap-unblock")
	planID := mustCreateTestPlan(t, s, projectID, "cap-unblock", "Cap", requiresFanoutPlan)

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "done"}}}
	// A resolver whose capability an operator can "fix" between passes.
	capable := false
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(func(_ context.Context, _ string, _ bool, _ BindingResolutionPurpose) (provider.Binding, error) {
			return provider.Binding{
				Name:   "claude",
				Config: provider.BindingConfig{Type: "claude", Binary: "true", NativeFanout: capable},
			}, nil
		}),
	)

	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("first DispatchNext: %v", err)
	}
	o.Wait()
	task, err := s.GetTask(ctx, planID, "needs-fanout")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != store.TaskStatusBlockedCapability {
		t.Fatalf("status = %q, want the task blocked first", task.Status)
	}

	// The operator binds the project to a fan-out-capable provider.
	capable = true

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("second DispatchNext: %v", err)
	}
	o.Wait()
	if dispatched != 1 {
		t.Fatalf("dispatched = %d after the binding was fixed, want 1 — a block "+
			"that survives its own remedy is a permanent stall, not a gate", dispatched)
	}
	if calls := runner.callReqs(); len(calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(calls))
	}
}

// TestCapabilityBlockEmitsOnlyOnTransition keeps the event stream usable.
//
// Blocked tasks are non-terminal and reconsidered on EVERY supervisor tick, so
// emitting per pass appends an identical event forever — the operator watching
// for real activity sees a flood, and the one signal that matters (the moment it
// became blocked) is buried.
func TestCapabilityBlockEmitsOnlyOnTransition(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "cap-events")
	planID := mustCreateTestPlan(t, s, projectID, "cap-events", "Cap", requiresFanoutPlan)

	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) {
			return &fakeRunner{}, nil
		}),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	for pass := 0; pass < 3; pass++ {
		if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
			t.Fatalf("DispatchNext pass %d: %v", pass, err)
		}
		o.Wait()
	}

	if got := countBlockedCapabilityEvents(t, s, projectID); got != 1 {
		t.Fatalf("got %d task.blocked_capability events across 3 passes, want 1 — "+
			"an unchanged block must not re-emit on every tick", got)
	}
}

// TestMaterializeBackfillsMetadataForAPreexistingTask covers the second P1: a
// task created before this change (or by an older dispatch path) has no
// task_metadata row, and migration 0003 does not backfill.
//
// materializeStepTask returned such a task early, skipping the metadata write,
// so MarkBlockedCapability then failed with ErrTaskMetadataMissing — and
// because the gate treats that as fatal, ONE legacy task aborted every dispatch
// pass for the whole plan.
func TestMaterializeBackfillsMetadataForAPreexistingTask(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "cap-legacy")
	planID := mustCreateTestPlan(t, s, projectID, "cap-legacy", "Cap", requiresFanoutPlan)

	// Simulate the legacy row: the task exists, the metadata row does not.
	if err := s.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "needs-fanout", Description: "delegate the whole group",
	}); err != nil {
		t.Fatalf("seed legacy task: %v", err)
	}
	if _, err := s.GetTaskExecutionMetadata(ctx, planID, "needs-fanout"); err == nil {
		t.Fatal("seed produced a metadata row; this test needs a task WITHOUT one")
	}

	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) {
			return &fakeRunner{}, nil
		}),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext aborted on a task with no metadata row: %v — one "+
			"legacy task must not stall the whole plan", err)
	}
	o.Wait()

	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "needs-fanout")
	if err != nil {
		t.Fatalf("metadata was not backfilled: %v", err)
	}
	if meta.BlockedReason == "" {
		t.Error("BlockedReason is empty; the backfilled row must still record why")
	}
}

// countBlockedCapabilityEvents counts the emitted block events for one plan.
func countBlockedCapabilityEvents(t *testing.T, s *store.Store, projectID string) int {
	t.Helper()
	events, err := s.ListProjectEvents(context.Background(), projectID, 200)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	n := 0
	for _, e := range events {
		if e.Kind == "task.blocked_capability" {
			n++
		}
	}
	return n
}

// TestClearingACapabilityBlockLeavesAnInputBlockIntact is a Major on #247.
//
// ClearTaskBlock cleared BOTH fail-closed states, and this gate only re-checks
// capabilities. So a task blocked on an immutable-input mismatch would be
// released the moment its (unrelated) capability requirement became
// satisfiable — dispatching work whose input pin is still violated, which is
// the exact admission failure blocked_input exists to prevent.
//
// A block must only be cleared by the condition that caused it.
func TestClearingACapabilityBlockLeavesAnInputBlockIntact(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "clear-scope")
	planID := mustCreateTestPlan(t, s, projectID, "clear-scope", "Cap", requiresFanoutPlan)

	// Start with an INCAPABLE binding so the first pass materializes the task
	// and blocks it on capability rather than running it to completion.
	capable := false
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) {
			return &fakeRunner{results: []provider.Result{{AssistantOutput: "x"}}}, nil
		}),
		WithBindingResolver(func(_ context.Context, _ string, _ bool, _ BindingResolutionPurpose) (provider.Binding, error) {
			return provider.Binding{
				Name:   "claude",
				Config: provider.BindingConfig{Type: "claude", Binary: "true", NativeFanout: capable},
			}, nil
		}),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("seed DispatchNext: %v", err)
	}
	o.Wait()

	// Now re-block it on INPUT — a DIFFERENT cause this gate never re-checks.
	if _, err := s.MarkBlockedInput(ctx, planID, "needs-fanout", "input digest mismatch"); err != nil {
		t.Fatalf("MarkBlockedInput: %v", err)
	}

	// Satisfy the capability requirement, which triggers the clear path.
	capable = true

	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	task, err := s.GetTask(ctx, planID, "needs-fanout")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != store.TaskStatusBlockedInput {
		t.Fatalf("status = %q, want %q — a satisfied capability requirement must "+
			"not clear an INPUT block the gate never re-checked; the input pin is "+
			"still violated", task.Status, store.TaskStatusBlockedInput)
	}
}
