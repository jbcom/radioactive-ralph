package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// pinnedBindingPlan declares a per-task binding rather than a `providers` list.
// Both express the same operator intent -- run THIS task on THIS provider --
// and until now only one of them was enforced.
const pinnedBindingPlan = "# Pinned\n\n" +
	"- pinned work\n\n" +
	"   ```ralph-task\n   {\"id\": \"pinned\", \"binding\": {\"provider\": \"codex\"}}\n   ```\n"

// TestDeclaredBindingProviderIsEnforcedAtDispatch closes the dispatch half of
// the inert-`binding` defect.
//
// #282 made the declared pin visible to partitioning, so a coalesced fan-out
// turn can no longer swallow it. But visibility is not enforcement: a task
// pinned to codex still RAN on whatever the pool resolved, because nothing
// consulted the pin when choosing the binding.
//
// That is the same shape as `providers` before #272 and `differentFrom` before
// its enforcement landed: the plan declares a restriction, the import accepts
// it, provenance records a turn, and the restriction was never applied. A field
// with no consumer reads to its author as a guarantee.
func TestDeclaredBindingProviderIsEnforcedAtDispatch(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "binding-pin")
	planID := mustCreateTestPlan(t, s, projectID, "binding-pin", "Pinned", pinnedBindingPlan)

	// The pool rotates claude first. Only the pin can steer this to codex.
	runner := &fakeRunner{results: []provider.Result{
		{AssistantOutput: "a"}, {AssistantOutput: "b"},
	}}
	names := []string{"claude", "codex"}
	var next int
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(func(_ context.Context, _ string, _ bool, purpose BindingResolutionPurpose) (provider.Binding, error) {
			name := names[next%len(names)]
			if purpose == BindingDispatch {
				next++
			}
			return provider.Binding{
				Name:   name,
				Config: provider.BindingConfig{Type: name, Binary: "true"},
			}, nil
		}),
	)

	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	// Assert on recorded provenance -- what the task ACTUALLY ran on -- for the
	// same reason the independence tests do: a call-count shape passes with
	// enforcement disabled.
	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "pinned")
	if err != nil || meta.AssignedAlias == "" {
		return // correctly refused to run: no permitted provider available
	}
	if meta.AssignedAlias != "codex" {
		t.Fatalf("task pins binding.provider=codex but ran on %q; the declaration "+
			"imported clean, the turn was recorded, and the pin was never applied -- "+
			"the plan reads as restricted while getting whatever the pool resolved",
			meta.AssignedAlias)
	}
}

// TestUnpinnedTaskStillUsesThePool is the other half. Without it the test above
// is satisfiable by refusing every task whose pin is unmet, which would break
// ordinary unpinned dispatch.
func TestUnpinnedTaskStillUsesThePool(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "binding-unpinned")
	planID := mustCreateTestPlan(t, s, projectID, "binding-unpinned", "Free",
		"# Free\n\n- ordinary work\n\n   ```ralph-task\n   {\"id\": \"free\"}\n   ```\n")

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "done"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "free")
	if err != nil || meta.AssignedAlias == "" {
		t.Fatal("an UNPINNED task did not run: enforcing a declared binding must not " +
			"turn an absent declaration into a restriction that blocks ordinary work")
	}
}

// TestBindingPinMatchesTypeNotAlias pins that a pin names WHAT RUNS, not what a
// binding is called.
//
// The first implementation folded binding.provider into AllowedProviders, and
// CheckAllowedProviders matches an entry against the binding ALIAS or its type.
// So an alias merely NAMED "codex" -- backed by type "claude" -- satisfied
// binding.provider="codex" and ran the task on Claude. That honours the
// declaration's spelling over its meaning, which is the same vacuous-guarantee
// shape as an unenforced restriction: the plan reads as pinned, provenance
// records a turn, and the provider is not the one named.
//
// Alias matching is correct for `providers`, where an operator naming
// "reviewer" means that configured binding. It is wrong for a pin, so the two
// are now separate checks.
func TestBindingPinMatchesTypeNotAlias(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "pin-alias")
	planID := mustCreateTestPlan(t, s, projectID, "pin-alias", "Pinned", pinnedBindingPlan)

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "a"}, {AssistantOutput: "b"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(func(_ context.Context, _ string, _ bool, _ BindingResolutionPurpose) (provider.Binding, error) {
			// Alias is "codex"; the binding actually runs CLAUDE.
			return provider.Binding{
				Name:   "codex",
				Config: provider.BindingConfig{Type: "claude", Binary: "true"},
			}, nil
		}),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "pinned")
	if err != nil || meta.AssignedAlias == "" {
		return // correctly refused: no binding has provider type codex
	}
	if meta.AssignedProvider != "codex" {
		t.Fatalf("task pins binding.provider=codex but ran on provider TYPE %q "+
			"(alias %q); an alias merely NAMED codex is not codex, so the pin was "+
			"satisfied by a lookalike", meta.AssignedProvider, meta.AssignedAlias)
	}
}
