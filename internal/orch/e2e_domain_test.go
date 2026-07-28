package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestOperatorRecordedCalibrationReachesTheTask is the END-TO-END proof that the
// producer and the consumer actually meet.
//
// Each half was verified in isolation on its own PR — #263 that a recorded
// calibration is readable by alias, #262 that dispatch stamps the domain it
// finds. Neither proves they COMPOSE: the producer writes what an operator
// supplies, the consumer requires the InvocationHash to equal what dispatch
// resolves, and nothing until now checked that an operator can actually produce
// a record satisfying that gate. If they disagreed, every calibration would be
// silently treated as stale and the domain would stay empty — the same vacuous
// guarantee, reached by a longer route.
func TestOperatorRecordedCalibrationReachesTheTask(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "e2e-domain")
	planID := mustCreateTestPlan(t, s, projectID, "e2e-domain", "E2E", provenancePlan)

	binding, err := fakeBindingResolver("claude", false)(ctx, "", false, BindingProbe)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	// Exactly what an operator has to compute to satisfy the dispatch-side gate.
	invocation, err := provider.ResolveInvocation(binding, provider.Request{})
	if err != nil {
		t.Fatalf("ResolveInvocation: %v", err)
	}
	hash, err := provider.InvocationConfigHash(
		binding, provider.Model(invocation.Model), invocation.Effort)
	if err != nil {
		t.Fatalf("InvocationConfigHash: %v", err)
	}

	// Exactly the store call the calibration-put handler makes (#263). Written
	// against the store API rather than that handler so this test lands with the
	// CONSUMER, which owns the invocation gate being verified.
	if _, err := s.RecordCalibration(ctx, store.ProviderCalibration{
		Alias: "claude", Provider: "claude",
		Model: invocation.Model, Effort: invocation.Effort,
		BinaryPath: "/usr/bin/claude", BinaryVersion: "1.0", BinarySHA256: "abc",
		InvocationHash:  hash,
		InferenceDomain: "anthropic", ControlDomain: "anthropic",
		IndependenceDomain: "anthropic",
	}); err != nil {
		t.Fatalf("RecordCalibration: %v", err)
	}

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "done"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "the-task")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if meta.AssignedIndependenceDomain != "anthropic" {
		t.Fatalf("domain = %q, want %q — the operator-recorded calibration did NOT "+
			"reach the task. Producer and consumer disagree about the invocation "+
			"identity, so every calibration is treated as stale and differentFrom "+
			"stays vacuous", meta.AssignedIndependenceDomain, "anthropic")
	}
}
