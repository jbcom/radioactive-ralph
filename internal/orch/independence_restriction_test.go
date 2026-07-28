package orch

import (
	"context"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/provider"
)

// restrictedIndependencePlan combines BOTH restrictions on one task: it must run
// on codex, and it must not share a domain with the task that produced the work.
const restrictedIndependencePlan = "# Restricted cross-check\n\n" +
	"- produce it\n\n" +
	"   ```ralph-task\n   {\"id\": \"produce\"}\n   ```\n\n" +
	"- review it\n\n" +
	"   ```ralph-task\n   {\"id\": \"review\", \"providers\": [\"codex\"], " +
	"\"differentFrom\": [\"produce\"]}\n   ```\n"

// TestIndependenceRotationHonorsAllowedProviders pins that the two restrictions
// compose instead of the later one silently overriding the earlier.
//
// dispatchReadyStep applies `providers` first and `differentFrom` second, as two
// independent rotations. The second returns on domain ALONE, so it can hand back
// a binding the first pass would have refused -- a task pinned to codex runs on
// claude, and the restriction that was written down is simply not applied.
//
// This matters most in exactly the case that motivates writing both: an operator
// who pins a reviewer to a specific provider AND demands independence gets a
// reviewer on neither the pinned provider nor, necessarily, an allowed one.
func TestIndependenceRotationHonorsAllowedProviders(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "indep-restricted")
	planID := mustCreateTestPlan(t, s, projectID, "indep-restricted", "Cross",
		restrictedIndependencePlan)

	// claude and codex share NO domain, so the independence constraint is
	// satisfiable -- but only claude's domain differs from what produce will use.
	// The rotation is therefore tempted to pick claude, which `providers` forbids.
	seedMatchingCalibration(t, s, "codex", "shared", false)
	seedMatchingCalibration(t, s, "claude", "distinct", false)

	runner := &fakeRunner{results: []provider.Result{
		{AssistantOutput: "produced"}, {AssistantOutput: "reviewed"},
	}}
	names := []string{"codex", "claude"}
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

	for pass := 0; pass < 2; pass++ {
		if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
			t.Fatalf("DispatchNext pass %d: %v", pass, err)
		}
		o.Wait()
	}

	review, err := s.GetTaskExecutionMetadata(ctx, planID, "review")
	if err != nil || review.AssignedAlias == "" {
		// Refusing to run is CORRECT here: no provider is both permitted and
		// independent, and deferring beats violating a written restriction.
		return
	}
	if review.AssignedAlias != "codex" {
		t.Fatalf("review ran on %q, but the task restricts providers to [codex] -- "+
			"the independence rotation overrode a restriction the operator wrote down, "+
			"so neither guarantee holds", review.AssignedAlias)
	}
}

// paddedPeerPlan names its peer with surrounding whitespace.
const paddedPeerPlan = "# Padded cross-check\n\n" +
	"- produce it\n\n" +
	"   ```ralph-task\n   {\"id\": \"produce\"}\n   ```\n\n" +
	"- review it\n\n" +
	"   ```ralph-task\n   {\"id\": \"review\", \"differentFrom\": [\" produce \"]}\n   ```\n"

// TestPaddedPeerReferenceIsRejectedAtImport pins that a peer reference which
// can never resolve is refused where the author can still fix it.
//
// Import validation compares strings.TrimSpace(raw), but the UNTRIMMED value is
// what stays in metadata and what dispatch hands to GetTaskExecutionMetadata as
// a task-ID lookup key. So `differentFrom: [" produce "]` passed validation and
// then failed its lookup on every tick -- forever, since no amount of running
// "produce" ever creates a task named " produce ".
//
// The failure mode was the quiet one: not rejected, not marked blocked, not
// reported anywhere. The step simply never became dispatchable while the plan
// read as valid. Rejecting beats trimming here for the same reason this parser
// already rejects nulls and duplicate keys -- silently repairing input means a
// plan that reads one way runs another.
func TestPaddedPeerReferenceIsRejectedAtImport(t *testing.T) {
	_, err := plan.Parse([]byte(paddedPeerPlan))
	if err == nil {
		t.Fatal("a differentFrom entry with surrounding whitespace imported CLEAN; it " +
			"is used verbatim as a task-ID lookup key, so it can never match a real " +
			"task and would defer its step permanently while the plan reads as valid")
	}
	if !strings.Contains(err.Error(), "whitespace") {
		t.Fatalf("import failed, but not for the whitespace: %v", err)
	}
}
