package orch

import (
	"context"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestTerminalProviderFailureIsNotRequeued is what makes failure classification
// change behavior rather than only wording. An invalid credential cannot
// succeed on a retry, so requeuing it burns the retry budget on turns that will
// fail identically and DELAYS the operator seeing a terminal error.
func TestTerminalProviderFailureIsNotRequeued(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "terminal-failure")
	planID := mustCreateTestPlan(t, s, projectID, "term", "Term", twoStepSequentialPlan)

	runner := &fakeRunner{errs: []error{provider.ErrClaudeAuthentication}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	tasks, err := s.ListTasks(ctx, planID, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	var failed, pending int
	for _, task := range tasks {
		switch task.Status {
		case store.TaskStatusFailed:
			failed++
		case store.TaskStatusPending:
			pending++
		}
	}
	if failed != 1 {
		t.Fatalf("failed tasks = %d, want 1 — an auth failure must be TERMINAL, "+
			"not requeued for three more turns against the same bad credential "+
			"(tasks = %+v)", failed, tasks)
	}
}

// TestRetryableProviderFailureIsRequeued is the control. Making auth terminal
// must not make everything terminal: a rate limit or an upstream fault is
// exactly what the retry budget exists for.
func TestRetryableProviderFailureIsRequeued(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "retryable-failure")
	planID := mustCreateTestPlan(t, s, projectID, "retry", "Retry", twoStepSequentialPlan)

	runner := &fakeRunner{errs: []error{provider.ErrClaudeRateLimit}}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("claude", false)),
	)

	if _, err := o.DispatchNext(ctx, projectID, planID); err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()

	tasks, err := s.ListTasks(ctx, planID, nil)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	for _, task := range tasks {
		if task.Status == store.TaskStatusFailed {
			t.Fatalf("a rate-limited turn failed terminally; waiting and retrying "+
				"is the remedy (tasks = %+v)", tasks)
		}
	}
}
