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
