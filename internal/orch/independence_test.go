package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// independencePlan declares a review task that must not share a provider with
// the task that produced the work.
const independencePlan = "# Cross-check\n\n" +
	"- produce the migration\n\n" +
	"   ```ralph-task\n   {\"id\": \"produce\"}\n   ```\n\n" +
	"- review it\n\n" +
	"   ```ralph-task\n   {\"id\": \"review\", \"differentFrom\": [\"produce\"]}\n   ```\n"

// calibrate records a calibration whose InvocationHash matches what dispatch
// resolves for this binding, so the domain is actually usable.
func calibrate(t *testing.T, s *store.Store, alias, domain string) {
	t.Helper()
	binding, err := fakeBindingResolver(alias, false)(
		context.Background(), "", false, BindingProbe)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	invocation, err := provider.ResolveInvocation(binding, provider.Request{})
	if err != nil {
		t.Fatalf("ResolveInvocation: %v", err)
	}
	hash, err := provider.InvocationConfigHash(
		binding, provider.Model(invocation.Model), invocation.Effort)
	if err != nil {
		t.Fatalf("InvocationConfigHash: %v", err)
	}
	if _, err := s.RecordCalibration(context.Background(), store.ProviderCalibration{
		Alias: alias, Provider: alias,
		Model: invocation.Model, Effort: invocation.Effort,
		BinaryPath: "/usr/bin/" + alias, BinaryVersion: "1.0", BinarySHA256: "abc",
		InvocationHash:  hash,
		InferenceDomain: domain, ControlDomain: domain,
		IndependenceDomain: domain,
	}); err != nil {
		t.Fatalf("RecordCalibration: %v", err)
	}
}

// TestIndependenceRefusesAPeerSharingItsDomain is the point of the field.
//
// Before this, differentFrom parsed, validated its references at import, and
// NOTHING compared domains at dispatch — so a plan asking for an independent
// review got one from the same provider that wrote the code, while reading as
// protected. That is the vacuous guarantee plan import already refuses to
// accept, and it is worse at dispatch because the plan looks enforced.
func TestIndependenceRefusesAPeerSharingItsDomain(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "indep-clash")
	planID := mustCreateTestPlan(t, s, projectID, "indep-clash", "Cross", independencePlan)

	// One provider in the pool, calibrated to a single domain: the reviewer can
	// only land on the same domain the producer used.
	calibrate(t, s, "claude", "anthropic")

	runner := &fakeRunner{results: []provider.Result{
		{AssistantOutput: "produced"}, {AssistantOutput: "reviewed"},
	}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	// Two passes: the steps are SEQUENTIAL, so pass 1 runs "produce" and pass 2
	// is the first pass in which "review" is eligible. A single pass can never
	// observe the constraint -- verified with a probe showing dispatched=1 and
	// review still unrun after one pass.
	for pass := 0; pass < 2; pass++ {
		if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
			t.Fatalf("DispatchNext pass %d: %v", pass, err)
		}
		o.Wait()
	}

	// Assert on RECORDED PROVENANCE, not on call counts. Both steps are one
	// parallel group, so a single pass dispatches both -- a "did a second pass
	// run anything" shape can never observe this and passes with enforcement
	// disabled. Verified: it did.
	review, err := s.GetTaskExecutionMetadata(ctx, planID, "review")
	if err == nil && review.AssignedAlias != "" {
		produce, perr := s.GetTaskExecutionMetadata(ctx, planID, "produce")
		if perr == nil && produce.AssignedIndependenceDomain != "" &&
			produce.AssignedIndependenceDomain == review.AssignedIndependenceDomain {
			t.Fatalf("review ran in domain %q, the SAME as produce -- differentFrom is "+
				"declared but not enforced, so the plan reads as protected while getting "+
				"a self-review", review.AssignedIndependenceDomain)
		}
	}
}

// TestIndependenceAllowsADistinctDomain is the other half, and without it the
// test above is satisfied by a dispatch that never runs anything.
func TestIndependenceAllowsADistinctDomain(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "indep-ok")
	planID := mustCreateTestPlan(t, s, projectID, "indep-ok", "Cross", independencePlan)

	// The pool rotates between two providers in DIFFERENT domains, so the
	// reviewer can land somewhere independent.
	calibrate(t, s, "claude", "anthropic")
	calibrate(t, s, "codex", "openai")

	runner := &fakeRunner{results: []provider.Result{
		{AssistantOutput: "produced"}, {AssistantOutput: "reviewed"},
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

	for pass := 0; pass < 2; pass++ {
		if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
			t.Fatalf("DispatchNext pass %d: %v", pass, err)
		}
		o.Wait()
	}
	review, err := s.GetTaskExecutionMetadata(ctx, planID, "review")
	if err != nil || review.AssignedAlias == "" {
		t.Fatal("review did NOT run even though the pool has a provider in a " +
			"different independence domain -- the constraint is blocking work it " +
			"should permit, which is as broken as failing to block")
	}
	produce, err := s.GetTaskExecutionMetadata(ctx, planID, "produce")
	if err == nil && produce.AssignedIndependenceDomain == review.AssignedIndependenceDomain {
		t.Fatalf("review ran in the same domain %q as produce", review.AssignedIndependenceDomain)
	}
}

// TestIndependenceDefersOnAnUncalibratedPeer pins the decision that makes the
// guarantee non-vacuous.
//
// An empty domain means the peer has not run, or ran on a binding nothing
// calibrated. Either way the domains are not KNOWN to differ, and a constraint
// that passes on absence of evidence enforces nothing while reading as
// protection.
func TestIndependenceDefersOnAnUncalibratedPeer(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "indep-uncal")
	planID := mustCreateTestPlan(t, s, projectID, "indep-uncal", "Cross", independencePlan)

	// NO calibration recorded: the producer runs, but its domain is empty.
	runner := &fakeRunner{results: []provider.Result{
		{AssistantOutput: "produced"}, {AssistantOutput: "reviewed"},
	}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	for pass := 0; pass < 2; pass++ {
		if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
			t.Fatalf("DispatchNext pass %d: %v", pass, err)
		}
		o.Wait()
	}
	produced, err := s.GetTaskExecutionMetadata(ctx, planID, "produce")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata(produce): %v", err)
	}
	if produced.AssignedIndependenceDomain != "" {
		t.Fatalf("expected an EMPTY domain with no calibration, got %q",
			produced.AssignedIndependenceDomain)
	}

	review, rerr := s.GetTaskExecutionMetadata(ctx, planID, "review")
	if rerr == nil && review.AssignedAlias != "" {
		t.Fatal("review ran against a peer whose domain is UNKNOWN -- passing on " +
			"absence of evidence is exactly the vacuous guarantee this enforcement " +
			"exists to prevent")
	}
}
