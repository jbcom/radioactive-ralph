package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// TestProviderTurnsCarryTheContainmentRootWhenEnabled is the test that should
// have existed before internal/contain was written.
//
// The containment package had thorough behavioral tests proving the kernel
// refuses an escaping write — and ZERO production callers set ContainmentRoot,
// so no real provider turn was ever confined. A primitive nothing invokes is
// the same false assurance as the validation layer it was built to supplement,
// and it looked wired up because the lower layer was well tested.
//
// This asserts the WIRING: what a dispatched turn actually receives.
func TestProviderTurnsCarryTheContainmentRootWhenEnabled(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "contain-wiring")
	planID := mustCreateTestPlan(t, s, projectID, "contain-wiring", "W", threeStepParallelPlan)

	runner := &fakeRunner{results: []provider.Result{
		{AssistantOutput: "a"}, {AssistantOutput: "b"}, {AssistantOutput: "c"},
	}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
		WithProviderWriteContainment(true),
	)

	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	calls := runner.callReqs()
	if len(calls) == 0 {
		t.Fatal("no provider turn was dispatched")
	}
	for i, req := range calls {
		if req.ContainmentRoot == "" {
			t.Errorf("turn %d ran with an EMPTY ContainmentRoot while containment "+
				"is enabled — the provider is unconfined and nothing reports it", i)
			continue
		}
		if req.ContainmentRoot != req.WorkingDir {
			t.Errorf("turn %d root = %q, want the project dir %q",
				i, req.ContainmentRoot, req.WorkingDir)
		}
	}
}

// TestProviderTurnsAreUnconfinedByDefault is the compatibility half. Containment
// changes what an existing deployment's provider may do, so it must not switch
// on merely because the code shipped.
func TestProviderTurnsAreUnconfinedByDefault(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "contain-default")
	planID := mustCreateTestPlan(t, s, projectID, "contain-default", "W", threeStepParallelPlan)

	runner := &fakeRunner{results: []provider.Result{
		{AssistantOutput: "a"}, {AssistantOutput: "b"}, {AssistantOutput: "c"},
	}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	for i, req := range runner.callReqs() {
		if req.ContainmentRoot != "" {
			t.Errorf("turn %d was confined to %q without the operator enabling it; "+
				"a provider that legitimately writes outside the checkout would "+
				"start failing on upgrade", i, req.ContainmentRoot)
		}
	}
}
