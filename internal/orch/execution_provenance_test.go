package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

const provenancePlan = "# Provenance\n\n" +
	"- do the thing\n\n" +
	"   ```ralph-task\n   {\"id\": \"the-task\"}\n   ```\n"

// TestDispatchRecordsWhatTheTaskRanOn is the write half of the provenance the
// store has carried since #236.
//
// Before this, RecordTaskExecution had ZERO production callers: the columns
// existed, the tests filled them in, and every real run left
// assigned_independence_domain empty. Any differentFrom enforcement built on
// that would have compared "" against "" and permitted everything — the vacuous
// guarantee plan import already refuses to accept, reintroduced at dispatch
// where it is harder to see. So this asserts the row is actually populated by a
// dispatch, not that a function exists.
func TestDispatchRecordsWhatTheTaskRanOn(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "prov")
	planID := mustCreateTestPlan(t, s, projectID, "prov", "Prov", provenancePlan)

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
	if meta.AssignedAlias == "" {
		t.Fatal("assigned_alias is empty after a dispatch — nothing recorded what " +
			"the task ran on, so provenance is unpopulated in exactly the way that " +
			"makes a differentFrom comparison vacuous")
	}
	if meta.AssignedAlias != "claude" {
		t.Errorf("assigned_alias = %q, want the binding that ran it", meta.AssignedAlias)
	}
	if meta.AssignedSessionID == "" {
		t.Error("assigned_session_id is empty — the record does not say WHICH session ran it")
	}
}

// TestDispatchRecordsTheCalibratedIndependenceDomain covers the field the whole
// exercise is for. The domain is not derived from the provider name — inferring
// claude→"anthropic" in dispatch would put a vendor table in the hot path and
// disagree with whatever a calibration later measures — so it comes from the
// binding's calibration record when one exists.
func TestDispatchRecordsTheCalibratedIndependenceDomain(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "prov-domain")
	planID := mustCreateTestPlan(t, s, projectID, "prov-domain", "Prov", provenancePlan)

	if _, err := s.RecordCalibration(ctx, store.ProviderCalibration{
		Alias: "claude", Provider: "claude", Model: "sonnet", Effort: "medium",
		BinaryPath: "/usr/bin/claude", BinaryVersion: "1.0", BinarySHA256: "abc",
		InvocationHash:  "hash",
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
		t.Fatalf("assigned_independence_domain = %q, want %q from the binding's "+
			"calibration — an empty domain here is what makes differentFrom compare "+
			"\"\" against \"\" and permit everything",
			meta.AssignedIndependenceDomain, "anthropic")
	}
}

// TestFanoutRecordsProvenanceForEveryTaskInTheGroup covers the case that
// matters most for differentFrom, and the one an obvious implementation gets
// wrong.
//
// A native fan-out runs the WHOLE group in one turn on one binding. Recording
// only the group's first task would leave its peers with an empty domain — and
// an empty domain reads as "independent", so the constraint would be satisfied
// by exactly the tasks that most obviously violate it: peers that provably
// shared a provider.
func TestFanoutRecordsProvenanceForEveryTaskInTheGroup(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "prov-fanout")
	planID := mustCreateTestPlan(t, s, projectID, "prov-fanout", "Fan", twoStepParallelPlan)

	if _, err := s.RecordCalibration(ctx, store.ProviderCalibration{
		Alias: "claude", Provider: "claude", Model: "sonnet", Effort: "medium",
		BinaryPath: "/usr/bin/claude", BinaryVersion: "1.0", BinarySHA256: "abc",
		InvocationHash:  "hash",
		InferenceDomain: "anthropic", ControlDomain: "anthropic",
		IndependenceDomain: "anthropic",
	}); err != nil {
		t.Fatalf("RecordCalibration: %v", err)
	}

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "done"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", true)), // NativeFanout
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	// nil statuses: every task regardless of state. The group's tasks may be
	// done, running, or failed by now, and provenance must be recorded for all
	// of them either way.
	tasks, err := s.ListTasks(ctx, planID, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) < 2 {
		t.Fatalf("plan has %d tasks, want the 2-step parallel group", len(tasks))
	}
	for _, task := range tasks {
		meta, err := s.GetTaskExecutionMetadata(ctx, planID, task.ID)
		if err != nil {
			t.Fatalf("GetTaskExecutionMetadata(%s): %v", task.ID, err)
		}
		if meta.AssignedIndependenceDomain != "anthropic" {
			t.Errorf("task %q domain = %q, want %q — every task in a fan-out shares "+
				"the group's binding, and a peer left empty reads as independent",
				task.ID, meta.AssignedIndependenceDomain, "anthropic")
		}
	}
}

// TestDispatchSucceedsWithoutACalibration pins the degenerate case. Most
// bindings are never calibrated, and an uncalibrated one must still dispatch
// and still record the rest of its provenance — an empty domain is honest
// ("nothing has established what this is independent of"), not an error.
func TestDispatchSucceedsWithoutACalibration(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "prov-nocal")
	planID := mustCreateTestPlan(t, s, projectID, "prov-nocal", "Prov", provenancePlan)

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "done"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)
	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext with no calibration: %v", err)
	}
	o.Wait()
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1 — a missing calibration must not block a turn", dispatched)
	}

	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "the-task")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if meta.AssignedIndependenceDomain != "" {
		t.Errorf("domain = %q, want empty when nothing has calibrated the binding",
			meta.AssignedIndependenceDomain)
	}
	if meta.AssignedAlias != "claude" {
		t.Errorf("alias = %q — the rest of the provenance must still be recorded", meta.AssignedAlias)
	}
}
