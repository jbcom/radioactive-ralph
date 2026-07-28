package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// independentFanoutPlan puts two MUTUALLY constrained tasks in one parallel
// group, alongside an unconstrained third.
//
// A parallel group is exactly what native fan-out coalesces, so this is the
// shape where "one binding for the whole group" and "these two must not share a
// binding" contradict each other.
const independentFanoutPlan = "# Cross-check wide\n\n" +
	"- write it\n\n" +
	"   ```ralph-task\n   {\"id\": \"write\"}\n   ```\n\n" +
	"- audit it\n\n" +
	"   ```ralph-task\n   {\"id\": \"audit\", \"differentFrom\": [\"write\"]}\n   ```\n\n" +
	"- unrelated chore\n\n" +
	"   ```ralph-task\n   {\"id\": \"chore\"}\n   ```\n"

// TestNativeFanoutDoesNotCoalesceIndependenceConstrainedTasks is the proof for
// the hole native fan-out opened in differentFrom.
//
// DispatchNext decides fan-out BEFORE the per-step admission loop, and returns
// from that branch — so the independence check in dispatchReadyStep is never
// reached for a coalesced group. Every claimed step then runs on ONE binding and
// runFanoutGroup stamps that one domain onto all of them.
//
// That is the vacuous guarantee in its worst form: the tasks are recorded as
// having run, with domains, and the plan reads as protected — while the audit
// was performed by the very provider that wrote the thing, in a single turn that
// saw both prompts. Rotation cannot fix it, because a fan-out group shares one
// turn by construction; the group must not coalesce constrained members at all.
func TestNativeFanoutDoesNotCoalesceIndependenceConstrainedTasks(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "fanout-indep")
	planID := mustCreateTestPlan(t, s, projectID, "fanout-indep", "Cross", independentFanoutPlan)

	// One calibrated provider that ALSO declares NativeFanout: the pool cannot
	// offer a second domain, so a correctly-enforced "audit" can never run here.
	//
	// nativeFanout=true is load-bearing, not cosmetic: it is part of the binding
	// config hash, so a calibration seeded with false describes a DIFFERENT
	// invocation, bindingDomain would read "" for it, and the domains would
	// never compare equal -- the test would pass without enforcing anything.
	seedMatchingCalibration(t, s, "claude", "anthropic", true)

	runner := &fakeRunner{results: []provider.Result{
		{AssistantOutput: "a"}, {AssistantOutput: "b"},
		{AssistantOutput: "c"}, {AssistantOutput: "d"},
	}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", true)), // NativeFanout
	)

	for pass := 0; pass < 3; pass++ {
		if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
			t.Fatalf("DispatchNext pass %d: %v", pass, err)
		}
		o.Wait()
	}

	// Assert on recorded provenance, for the same reason the per-step tests do:
	// a call-count shape passes with enforcement disabled.
	audit, aerr := s.GetTaskExecutionMetadata(ctx, planID, "audit")
	if aerr != nil || audit.AssignedAlias == "" {
		return // audit correctly never ran: no independent provider exists
	}
	write, werr := s.GetTaskExecutionMetadata(ctx, planID, "write")
	if werr == nil && write.AssignedIndependenceDomain != "" &&
		write.AssignedIndependenceDomain == audit.AssignedIndependenceDomain {
		t.Fatalf("audit ran in domain %q, the SAME as write, because native fan-out "+
			"coalesced them into one turn and never reached the independence check -- "+
			"the plan declares differentFrom and reads as protected while getting a "+
			"self-audit from the provider that wrote the work",
			audit.AssignedIndependenceDomain)
	}
}

// TestNativeFanoutStillCoalescesUnconstrainedTasks is the other half. Without
// it, the test above is satisfied by disabling fan-out entirely -- trading a
// silent correctness hole for a silent performance regression.
func TestNativeFanoutStillCoalescesUnconstrainedTasks(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "fanout-uncon")
	planID := mustCreateTestPlan(t, s, projectID, "fanout-uncon", "Fan", threeStepParallelPlan)

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "done"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", true)),
	)

	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	if got := len(runner.callReqs()); got != 1 {
		t.Fatalf("runner called %d times, want EXACTLY 1 -- a group with NO independence "+
			"constraints must still coalesce into one native fan-out turn; refusing to "+
			"fan out at all would be a silent performance regression", got)
	}
}
