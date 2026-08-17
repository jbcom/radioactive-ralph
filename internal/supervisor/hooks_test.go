package supervisor

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/a2a"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jonboulle/clockwork"
)

func TestHandleHookEventStopUsesLiveBindingAndIndependentAcceptance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX acceptance command")
	}
	ctx := context.Background()
	sup := newTestSupervisor(t, clockwork.NewFakeClock())
	projectID, err := sup.store.CreateProject(ctx, "hook-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	planID, err := sup.store.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID: projectID, Slug: "hook", Title: "Hook",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := sup.store.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "task", Description: "task", AcceptanceJSON: `{"command":"exit 0"}`,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := sup.store.PutTaskMetadata(ctx, planID, "task", "0", "", `{}`); err != nil {
		t.Fatalf("PutTaskMetadata: %v", err)
	}
	sessionID, err := sup.store.CreateSession(ctx, store.SessionOpts{Role: "worker", PID: 2, PIDStartTime: "t1"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := sup.store.CreateWorker(ctx, store.WorkerOpts{
		SessionID: sessionID, Provider: "claude", SubprocessPID: 3, SubprocessStartTime: "t1",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if _, err := sup.store.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if err := sup.store.RecordTaskExecution(ctx, planID, "task", "claude", "claude", "", "", "", sessionID); err != nil {
		t.Fatalf("RecordTaskExecution: %v", err)
	}

	stop, err := sup.HandleHookEvent(ctx, ipc.HookEventArgs{
		Adapter: "claude", Event: ipc.HookEventStop, SessionID: sessionID,
	})
	if err != nil || stop.Allow || stop.Reason != "verification_started" {
		t.Fatalf("first stop reply = %+v, err=%v", stop, err)
	}
	sup.orch.Wait()
	stop, err = sup.HandleHookEvent(ctx, ipc.HookEventArgs{
		Adapter: "claude", Event: ipc.HookEventStop, SessionID: sessionID,
	})
	if err != nil || !stop.Allow || stop.Reason != "acceptance_passed" {
		t.Fatalf("verified stop reply = %+v, err=%v", stop, err)
	}
	if mismatch, err := sup.HandleHookEvent(ctx, ipc.HookEventArgs{
		Adapter: "codex", Event: ipc.HookEventStop, SessionID: sessionID,
	}); err != nil || mismatch.Allow || mismatch.Reason != "adapter_mismatch" {
		t.Fatalf("mismatch reply = %+v, err=%v", mismatch, err)
	}
	task, err := sup.store.GetTask(ctx, planID, "task")
	if err != nil || task.Status != store.TaskStatusRunning {
		t.Fatalf("hook mutated task = %+v, err=%v", task, err)
	}

	// Any later tool progress invalidates the cached pass. A Stop after changing
	// checkout state must start a fresh independent check.
	progress, err := sup.HandleHookEvent(ctx, ipc.HookEventArgs{
		Adapter: "claude", Event: ipc.HookEventPostToolUse, SessionID: sessionID,
	})
	if err != nil || !progress.Allow || progress.Reason != "progress_recorded" {
		t.Fatalf("progress reply = %+v, err=%v", progress, err)
	}
	stop, err = sup.HandleHookEvent(ctx, ipc.HookEventArgs{
		Adapter: "claude", Event: ipc.HookEventStop, SessionID: sessionID,
	})
	if err != nil || stop.Allow || stop.Reason != "verification_started" {
		t.Fatalf("post-progress stop reply = %+v, err=%v", stop, err)
	}
	sup.orch.Wait()
}

func TestHandleHookEventDoesNotParkStopBehindAcceptance(t *testing.T) {
	ctx := context.Background()
	sup := newTestSupervisor(t, clockwork.NewFakeClock())
	sessionID := seedManagedHookTask(t, sup, `{"command":"slow"}`)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	sup.orch = orch.New(sup.store, orch.WithAcceptanceChecker(func(
		context.Context, string, string, a2a.Evidence,
	) (bool, string, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return true, "", nil
	}))

	first, err := sup.HandleHookEvent(ctx, ipc.HookEventArgs{
		Adapter: "claude", Event: ipc.HookEventStop, SessionID: sessionID,
	})
	if err != nil || first.Allow || first.Reason != "verification_started" {
		t.Fatalf("first Stop = %+v, err=%v", first, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("asynchronous acceptance did not start")
	}

	second, err := sup.HandleHookEvent(ctx, ipc.HookEventArgs{
		Adapter: "claude", Event: ipc.HookEventStop, SessionID: sessionID,
	})
	if err != nil || second.Allow || second.Reason != "verification_pending" {
		t.Fatalf("pending Stop = %+v, err=%v", second, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("pending Stop launched %d acceptance checks, want 1", got)
	}
	close(release)
	sup.orch.Wait()
	third, err := sup.HandleHookEvent(ctx, ipc.HookEventArgs{
		Adapter: "claude", Event: ipc.HookEventStop, SessionID: sessionID,
	})
	if err != nil || !third.Allow || third.Reason != "acceptance_passed" {
		t.Fatalf("verified Stop = %+v, err=%v", third, err)
	}
}

func TestHandleHookEventSerializesProgressInvalidationBeforeStopVerdict(t *testing.T) {
	ctx := context.Background()
	sup := newTestSupervisor(t, clockwork.NewFakeClock())
	sessionID := seedManagedHookTask(t, sup, `{"command":"exit 0"}`)
	tasks, err := sup.store.RunningHookTasks(ctx, sessionID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("RunningHookTasks = %+v, err=%v", tasks, err)
	}
	written, err := sup.store.SetHookVerificationPending(
		ctx, sessionID, tasks[0].PlanID, tasks[0].TaskID)
	if err != nil || !written {
		t.Fatalf("SetHookVerificationPending: written=%v err=%v", written, err)
	}
	if err := sup.store.SetHookVerificationResult(
		ctx, sessionID, tasks[0].PlanID, tasks[0].TaskID, true); err != nil {
		t.Fatalf("SetHookVerificationResult: %v", err)
	}

	invalidationEntered := make(chan struct{})
	releaseInvalidation := make(chan struct{})
	sup.beforeHookInvalidation = func() {
		close(invalidationEntered)
		<-releaseInvalidation
	}
	t.Cleanup(func() {
		sup.beforeHookInvalidation = nil
		select {
		case <-releaseInvalidation:
		default:
			close(releaseInvalidation)
		}
	})

	progressDone := make(chan ipc.HookEventReply, 1)
	go func() {
		reply, _ := sup.HandleHookEvent(ctx, ipc.HookEventArgs{
			Adapter: "claude", Event: ipc.HookEventPostToolUse, SessionID: sessionID,
		})
		progressDone <- reply
	}()
	select {
	case <-invalidationEntered:
	case <-time.After(time.Second):
		t.Fatal("progress hook did not reach invalidation seam")
	}

	stopDone := make(chan ipc.HookEventReply, 1)
	go func() {
		reply, _ := sup.HandleHookEvent(ctx, ipc.HookEventArgs{
			Adapter: "claude", Event: ipc.HookEventStop, SessionID: sessionID,
		})
		stopDone <- reply
	}()
	select {
	case reply := <-stopDone:
		t.Fatalf("Stop raced past in-flight progress invalidation: %+v", reply)
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseInvalidation)
	if reply := <-progressDone; !reply.Allow || reply.Reason != "progress_recorded" {
		t.Fatalf("progress reply = %+v", reply)
	}
	if reply := <-stopDone; reply.Allow || reply.Reason != "verification_started" {
		t.Fatalf("post-invalidation Stop reply = %+v", reply)
	}
	sup.orch.Wait()
}

func seedManagedHookTask(t *testing.T, sup *Supervisor, acceptance string) string {
	t.Helper()
	ctx := context.Background()
	projectID, err := sup.store.CreateProject(ctx, "async-hook-project", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	planID, err := sup.store.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID: projectID, Slug: "async-hook", Title: "Async hook",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := sup.store.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "task", Description: "task", AcceptanceJSON: acceptance,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := sup.store.PutTaskMetadata(ctx, planID, "task", "0", "", `{}`); err != nil {
		t.Fatalf("PutTaskMetadata: %v", err)
	}
	sessionID, err := sup.store.CreateSession(ctx, store.SessionOpts{
		Role: "worker", PID: 20, PIDStartTime: "async",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := sup.store.CreateWorker(ctx, store.WorkerOpts{
		SessionID: sessionID, Provider: "claude", SubprocessPID: 21, SubprocessStartTime: "async",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if _, err := sup.store.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if err := sup.store.RecordTaskExecution(
		ctx, planID, "task", "claude", "claude", "", "", "", sessionID,
	); err != nil {
		t.Fatalf("RecordTaskExecution: %v", err)
	}
	return sessionID
}
