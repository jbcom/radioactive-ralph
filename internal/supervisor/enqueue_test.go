package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// fakeRunner is a minimal provider.Runner test double — mirrors
// internal/orch's own fakeRunner but lives here since that one is
// unexported to its package. It returns a canned successful Result for
// every call so DispatchNext's mechanical, judgment-only fallback
// (non-empty evidence output) always accepts.
type fakeRunner struct {
	calls int
}

func (f *fakeRunner) Run(context.Context, provider.Binding, provider.Request) (provider.Result, error) {
	f.calls++
	return provider.Result{AssistantOutput: "did the work"}, nil
}

// TestHandleEnqueue_DispatchesSeededPlan is the proof for task 5 of the
// Phase 6c tech-debt pass: HandleEnqueue must actually dispatch, not
// return "not implemented". A plan with one ready step is seeded directly
// in the store; a real Supervisor (constructed the same way Run does, via
// runSupervisorInBackground/Options) is given an Orchestrator wired to a
// fake runner; calling client.Enqueue must cause that one step to be
// dispatched and marked done.
func TestHandleEnqueue_DispatchesSeededPlan(t *testing.T) {
	runtimeDir := t.TempDir()
	st := openTestStore(t)
	ctx := context.Background()

	projectID, err := st.CreateProject(ctx, "enqueue-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: "/tmp/enqueue-project"},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	planID, err := st.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "enqueue-plan",
		Title:          "Ship",
		SourceMarkdown: "# Ship\n\n1. write the code\n",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := st.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}

	runner := &fakeRunner{}
	o := orch.New(st,
		orch.WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		orch.WithBindingResolver(func(context.Context, string, bool) (provider.Binding, error) {
			return provider.Binding{Name: "claude", Config: provider.BindingConfig{Type: "claude", Binary: "true"}}, nil
		}),
	)

	cancel, done := runSupervisorInBackground(t, Options{RuntimeDir: runtimeDir, Store: st, Orchestrator: o})
	defer cancel()

	client := waitForSupervisor(t, runtimeDir, 2*time.Second)
	defer func() { _ = client.Close() }()

	reply, err := client.Enqueue(context.Background(), ipc.EnqueueArgs{Description: "kick the plan"})
	if err != nil {
		t.Fatalf("client.Enqueue: %v", err)
	}
	if !reply.Inserted {
		t.Errorf("EnqueueReply.Inserted = false, want true (the seeded plan's ready step should have been dispatched)")
	}
	if runner.calls != 1 {
		t.Fatalf("fakeRunner.calls = %d, want 1 — HandleEnqueue must actually dispatch, not no-op", runner.calls)
	}

	task, err := st.GetTask(ctx, planID, "0.0")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != store.TaskStatusDone {
		t.Errorf("task status = %q, want done after the enqueue-triggered dispatch was verified", task.Status)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error after ctx cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not exit within 3s of ctx cancel")
	}
}

// TestHandleEnqueue_NoActivePlansReturnsNotInserted confirms an enqueue
// against a supervisor with no active plans at all is a clean, honest
// "nothing to dispatch" rather than an error or a false-positive success.
func TestHandleEnqueue_NoActivePlansReturnsNotInserted(t *testing.T) {
	runtimeDir := t.TempDir()
	st := openTestStore(t)

	cancel, _ := runSupervisorInBackground(t, Options{RuntimeDir: runtimeDir, Store: st})
	defer cancel()

	client := waitForSupervisor(t, runtimeDir, 2*time.Second)
	defer func() { _ = client.Close() }()

	reply, err := client.Enqueue(context.Background(), ipc.EnqueueArgs{Description: "nothing to do"})
	if err != nil {
		t.Fatalf("client.Enqueue: %v", err)
	}
	if reply.Inserted {
		t.Error("EnqueueReply.Inserted = true, want false with no active plans")
	}
}
