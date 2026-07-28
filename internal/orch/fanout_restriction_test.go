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
