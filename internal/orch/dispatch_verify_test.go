package orch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestDispatchNextWorkerTerminationAloneIsNotCompletion is the end-to-end
// version of the completion-verification test: DispatchNext runs a worker
// whose provider turn "terminates" (Runner.Run returns normally) with
// empty/insufficient evidence. Because there is no acceptance criterion to
// mechanically re-run and the evidence output is empty, VerifyAndComplete
// must reject it — a worker terminating is NOT completion.
func TestDispatchNextWorkerTerminationAloneIsNotCompletion(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "term-project")
	planID := mustCreateTestPlan(t, s, projectID, "term-plan", "Ship", twoStepSequentialPlan)

	// The fake runner terminates "successfully" (no error) but produces
	// NO assistant output at all — modeling a worker process that exits
	// cleanly without having done (or reported) anything.
	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: ""}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1", dispatched)
	}
	o.Wait() // dispatch is async — wait for the turn + verification to land

	task, err := s.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status == store.TaskStatusDone {
		t.Fatal("task marked done after a worker terminated with empty evidence — termination must never imply completion")
	}
	// Out of retries budget is 3 in VerifyAndComplete; one failed
	// verification should leave it pending (retryable) rather than failed.
	if task.Status != store.TaskStatusPending {
		t.Errorf("task status = %q, want pending (retryable after one rejected verification)", task.Status)
	}

	events, err := s.ListTaskEvents(ctx, planID, "0.0", 10)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind == "worker.verification_failed" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected worker.verification_failed after empty-evidence termination, got %+v", events)
	}
}

// TestDispatchNextRunnerErrorMarksFailedNotDone confirms a provider.Runner
// error (the underlying CLI failing) is routed to MarkFailed, never
// VerifyAndComplete/MarkDone.
func TestDispatchNextRunnerErrorMarksFailedNotDone(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "err-project")
	planID := mustCreateTestPlan(t, s, projectID, "err-plan", "Ship", twoStepSequentialPlan)

	runner := &fakeRunner{errs: []error{errRunnerBoom}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1", dispatched)
	}
	o.Wait() // dispatch is async — wait for the turn + failure handling to land

	task, err := s.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status == store.TaskStatusDone {
		t.Fatal("task marked done despite the provider Run erroring")
	}
}

type boomError struct{}

func (boomError) Error() string { return "boom" }

var errRunnerBoom = boomError{}

func TestDispatchPersistsOnlyClassifiedProviderFailure(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "private-failure-project")
	planID := mustCreateTestPlan(t, s, projectID, "private-failure-plan", "Ship", twoStepSequentialPlan)
	const secret = "SECRET-PROVIDER-DIAGNOSTIC"
	runner := &fakeRunner{errs: []error{errors.Join(provider.ErrProviderStalled, errors.New(secret))}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)
	if dispatched, err := o.DispatchNext(ctx, projectID, planID); err != nil || dispatched != 1 {
		t.Fatalf("DispatchNext = %d, %v", dispatched, err)
	}
	o.Wait()

	messages, err := s.ListMessages(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if strings.Contains(messages[0].ContentJSON, secret) {
		t.Fatal("raw provider diagnostic leaked into evidence")
	}
	if !strings.Contains(messages[0].ContentJSON, `"failure_category":"stall_timeout"`) {
		t.Fatalf("classified evidence missing category: %s", messages[0].ContentJSON)
	}

	events, err := s.ListTaskEvents(ctx, planID, "0.0", 10)
	if err != nil {
		t.Fatalf("ListTaskEvents: %v", err)
	}
	for _, event := range events {
		if strings.Contains(event.PayloadJSON, secret) {
			t.Fatalf("raw provider diagnostic leaked into event %s", event.Kind)
		}
	}
}

func TestDispatchRejectsOversizedProviderEvidenceBeforePersistence(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "bounded-evidence-project")
	planID := mustCreateTestPlan(t, s, projectID, "bounded-evidence-plan", "Ship", twoStepSequentialPlan)
	runner := &fakeRunner{results: []provider.Result{{AssistantOutput: strings.Repeat("x", 17<<20)}}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)
	if dispatched, err := o.DispatchNext(ctx, projectID, planID); err != nil || dispatched != 1 {
		t.Fatalf("DispatchNext = %d, %v", dispatched, err)
	}
	o.Wait()

	messages, err := s.ListMessages(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	if len(messages[0].ContentJSON) > 1024 {
		t.Fatalf("persisted evidence bytes = %d, want only bounded failure metadata", len(messages[0].ContentJSON))
	}
	if !strings.Contains(messages[0].ContentJSON, `"failure_category":"output_limit"`) {
		t.Fatalf("oversized evidence missing output_limit category: %s", messages[0].ContentJSON)
	}
}
