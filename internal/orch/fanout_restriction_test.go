package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// restrictedFanoutPlan pins ONE task in a parallel group to a specific provider
// while its neighbours are unrestricted.
const restrictedFanoutPlan = "# Restricted fan-out\n\n" +
	"- free work\n\n" +
	"   ```ralph-task\n   {\"id\": \"free\"}\n   ```\n\n" +
	"- pinned work\n\n" +
	"   ```ralph-task\n   {\"id\": \"pinned\", \"providers\": [\"codex\"]}\n   ```\n\n" +
	"- more free work\n\n" +
	"   ```ralph-task\n   {\"id\": \"free2\"}\n   ```\n"

// TestNativeFanoutDoesNotCoalesceProviderRestrictedTasks is the twin of the
// independence case, and it is the same defect wearing different clothes.
//
// coalescableSteps excluded independence-constrained steps but not
// provider-restricted ones, and dispatchFanoutGroup never calls
// resolveAllowedBinding or CheckAllowedProviders. So a task pinned to codex
// could be swept into a claude fan-out turn and executed there -- the exact
// class of silently-violated restriction the independence fix just closed, left
// open one field over.
//
// Fan-out is one binding for the whole group by construction, so a group cannot
// honour a member's `providers` unless that member is left out of it.
func TestNativeFanoutDoesNotCoalesceProviderRestrictedTasks(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "fanout-restricted")
	planID := mustCreateTestPlan(t, s, projectID, "fanout-restricted", "Fan",
		restrictedFanoutPlan)

	// The fan-out binding is claude -- which "pinned" forbids.
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

	// Assert on recorded provenance: what the task ACTUALLY ran on.
	pinned, err := s.GetTaskExecutionMetadata(ctx, planID, "pinned")
	if err != nil || pinned.AssignedAlias == "" {
		return // correctly never ran: no permitted provider is available
	}
	if pinned.AssignedAlias != "codex" {
		t.Fatalf("task \"pinned\" restricts providers to [codex] but ran on %q, because "+
			"native fan-out coalesced it into another provider's turn without ever "+
			"checking CheckAllowedProviders -- a restriction the operator wrote down, "+
			"silently not applied", pinned.AssignedAlias)
	}
}

// pinnedFanoutPlan pins ONE task in a parallel group via `binding.provider`
// rather than `providers`. Both express the same intent.
const pinnedFanoutPlan = "# Pinned fan-out\n\n" +
	"- free work\n\n" +
	"   ```ralph-task\n   {\"id\": \"free\"}\n   ```\n\n" +
	"- pinned work\n\n" +
	"   ```ralph-task\n   {\"id\": \"pinned\", \"binding\": {\"provider\": \"codex\"}}\n   ```\n\n" +
	"- more free work\n\n" +
	"   ```ralph-task\n   {\"id\": \"free2\"}\n   ```\n"

// TestNativeFanoutDoesNotCoalesceBindingPinnedTasks is the THIRD instance of
// one hole, and the one coalescableSteps' own doc comment predicted.
//
// That comment says a fourth per-step restriction "inherits this hole unless it
// is added here too". `binding.provider` was added as a restriction and NOT
// added there, so a task pinned to codex could be swept into a claude fan-out
// turn -- the same silent violation `providers` had until #272 and
// `differentFrom` had before it.
//
// Predicting a failure mode in a comment does not prevent it. The rule is
// stated once and must be APPLIED once: a fan-out group is one binding chosen
// before any member is examined, so NO per-step binding restriction survives
// coalescing.
func TestNativeFanoutDoesNotCoalesceBindingPinnedTasks(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "fanout-pinned")
	planID := mustCreateTestPlan(t, s, projectID, "fanout-pinned", "Fan", pinnedFanoutPlan)

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

	pinned, err := s.GetTaskExecutionMetadata(ctx, planID, "pinned")
	if err != nil || pinned.AssignedAlias == "" {
		return // correctly never ran: no binding of type codex is available
	}
	if pinned.AssignedProvider != "codex" {
		t.Fatalf("task pins binding.provider=codex but ran on provider type %q; "+
			"native fan-out coalesced it into another provider's turn, so the pin "+
			"imported clean, the turn was recorded, and the restriction was never "+
			"applied", pinned.AssignedProvider)
	}
}
