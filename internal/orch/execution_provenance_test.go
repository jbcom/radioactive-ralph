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

// seedMatchingCalibration records a calibration whose InvocationHash matches
// what dispatch will resolve for this binding.
//
// Computing the hash the same way dispatch does is the point. An earlier version
// of these tests wrote a literal InvocationHash, which passed only because
// nothing compared it; once dispatch started requiring a match, that literal
// described a command line no turn would ever run, and the domain was correctly
// withheld. A test fixture that cannot match reality proves nothing about it.
func seedMatchingCalibration(t *testing.T, s *store.Store, alias, domain string, nativeFanout bool) {
	t.Helper()
	binding, err := fakeBindingResolver(alias, nativeFanout)(
		context.Background(), "", false, BindingProbe)
	if err != nil {
		t.Fatalf("resolve binding for calibration fixture: %v", err)
	}
	invocation, err := provider.ResolveInvocation(binding, provider.Request{})
	if err != nil {
		t.Fatalf("resolve invocation for calibration fixture: %v", err)
	}
	hash, err := provider.InvocationConfigHash(
		binding, provider.Model(invocation.Model), invocation.Effort)
	if err != nil {
		t.Fatalf("invocation hash for calibration fixture: %v", err)
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

	seedMatchingCalibration(t, s, "claude", "anthropic", false)

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

	// NativeFanout=true is a DIFFERENT binding config, so it hashes
	// differently: the fixture must describe the binding this group will
	// actually run on, not merely share its alias.
	seedMatchingCalibration(t, s, "claude", "anthropic", true)

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

// TestStaleCalibrationDoesNotLeakItsDomain is the fix for a review finding, and
// the reason alias-matching alone was wrong.
//
// A calibration measures ONE command line — that is what InvocationHash
// identifies. Reusing its independence domain whenever the alias matches means a
// binding whose config, model, or effort changed since the probe stamps a domain
// derived from a command line that is no longer run. differentFrom would then
// accept or reject tasks on evidence about something else, which is worse than
// having no evidence: it looks measured.
func TestStaleCalibrationDoesNotLeakItsDomain(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "prov-stale")
	planID := mustCreateTestPlan(t, s, projectID, "prov-stale", "Prov", provenancePlan)

	// Calibrated for a DIFFERENT invocation than the one dispatch will resolve:
	// same alias, hash of a command line no turn here will run.
	if _, err := s.RecordCalibration(ctx, store.ProviderCalibration{
		Alias: "claude", Provider: "claude", Model: "sonnet", Effort: "medium",
		BinaryPath: "/usr/bin/claude", BinaryVersion: "1.0", BinarySHA256: "abc",
		InvocationHash:  "hash-of-some-other-command-line",
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
	if meta.AssignedIndependenceDomain != "" {
		t.Fatalf("domain = %q from a calibration measured for a DIFFERENT invocation; "+
			"want empty. A stale domain is worse than none — it makes differentFrom "+
			"decide on evidence about a command line this turn never ran",
			meta.AssignedIndependenceDomain)
	}
	// The rest of the provenance must still be recorded: the turn happened.
	if meta.AssignedAlias != "claude" {
		t.Errorf("alias = %q, want the binding that ran it", meta.AssignedAlias)
	}
}

// TestProvenanceRecordsTheResolvedModelNotTheRequestedOne is the second review
// fix. Dispatch does not pin a tier, so req.Model and req.Effort are empty on
// every turn; recording them wrote empty model/effort while appearing to have
// captured them. The binding's own mapping is what the provider actually runs.
func TestProvenanceRecordsTheResolvedModelNotTheRequestedOne(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "prov-model")
	planID := mustCreateTestPlan(t, s, projectID, "prov-model", "Prov", provenancePlan)

	// A binding that MAPS a tier. With the bare fake binding, resolved and
	// requested are both empty, so the assertion would compare "" against "" and
	// pass with the fix reverted — verified, it did. The mapping is what makes
	// the two values differ and the test able to fail.
	mapped := func(_ context.Context, _ string, _ bool, _ BindingResolutionPurpose) (provider.Binding, error) {
		return provider.Binding{
			Name: "claude",
			Config: provider.BindingConfig{
				Type: "claude", Binary: "true",
				SonnetModel: "claude-sonnet-mapped",
			},
		}, nil
	}

	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: "done"}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(mapped),
	)
	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	binding, err := mapped(ctx, "", false, BindingProbe)
	if err != nil {
		t.Fatalf("resolve binding: %v", err)
	}
	want, err := provider.ResolveInvocation(binding, provider.Request{})
	if err != nil {
		t.Fatalf("ResolveInvocation: %v", err)
	}
	if want.Model == "" {
		t.Fatal("fixture resolves to an empty model, so this test cannot distinguish " +
			"the resolved value from the requested one")
	}

	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "the-task")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if meta.AssignedModel != want.Model {
		t.Errorf("assigned_model = %q, want the RESOLVED %q — the record must say "+
			"which model produced the result, not which tier was asked for",
			meta.AssignedModel, want.Model)
	}
	if meta.AssignedEffort != want.Effort {
		t.Errorf("assigned_effort = %q, want the resolved %q", meta.AssignedEffort, want.Effort)
	}
}
