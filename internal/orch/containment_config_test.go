package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// TestContainmentIsResolvedPerProject is the wiring test for the config key.
//
// The key existed, vconfig parsed it, and NOTHING consulted it: an operator who
// set contain_provider_writes = true got no containment and no signal that the
// setting did nothing. A config that lies is worse than one that is absent,
// because it is trusted. This asserts the resolver's answer reaches the turn.
//
// Per PROJECT, not per process: one orchestrator serves every project on the
// host, so a static flag could only ever apply one project's answer to all of
// them — the same reason BindingResolver is a function.
func TestContainmentIsResolvedPerProject(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	confined := mustCreateTestProject(t, s, "contained")
	open := mustCreateTestProject(t, s, "uncontained")

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "done"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
		WithContainmentResolver(func(_ context.Context, projectID string) bool {
			return projectID == confined
		}),
	)

	for _, tc := range []struct {
		name      string
		projectID string
		want      bool
	}{
		{"containment on", confined, true},
		{"containment off", open, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(runner.callReqs())
			planID := mustCreateTestPlan(t, s, tc.projectID, "cfg-"+tc.name, "Cfg", containmentCfgPlan)
			if _, err := o.DispatchNext(ctx, tc.projectID, planID); err != nil {
				t.Fatalf("DispatchNext: %v", err)
			}
			o.Wait()
			// The fake accumulates across subtests, so compare against the count
			// taken before this dispatch rather than resetting it: a reset would
			// hide a turn that never ran at all.
			calls := runner.callReqs()
			if len(calls) <= before {
				t.Fatal("no turn ran for this project, so nothing was measured")
			}
			got := calls[len(calls)-1].ContainmentRoot != ""
			if got != tc.want {
				t.Fatalf("ContainmentRoot set = %v, want %v — the stored per-project "+
					"config did not reach the turn, so the key is inert", got, tc.want)
			}
		})
	}
}

const containmentCfgPlan = "# Containment config\n\n" +
	"- do the thing\n\n" +
	"   ```ralph-task\n   {\"id\": \"the-task\"}\n   ```\n"
