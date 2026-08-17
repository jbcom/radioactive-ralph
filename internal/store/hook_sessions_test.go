package store

import (
	"context"
	"testing"
)

func TestRunningHookTasksRequiresLiveClaimAndExecutionBinding(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "hook-project")
	planID := mustCreatePlan(t, s, projectID, "hook-plan")
	if err := s.CreateTask(ctx, CreateTaskOpts{
		PlanID: planID, ID: "task", Description: "task", AcceptanceJSON: `{"command":"exit 0"}`,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.PutTaskMetadata(ctx, planID, "task", "0", "", `{}`); err != nil {
		t.Fatalf("PutTaskMetadata: %v", err)
	}
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "hook")
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	tasks, err := s.RunningHookTasks(ctx, sessionID)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("claim without execution binding returned hook tasks = %+v, err=%v", tasks, err)
	}
	if err := s.RecordTaskExecution(ctx, planID, "task", "claude", "claude", "", "", "", sessionID); err != nil {
		t.Fatalf("RecordTaskExecution: %v", err)
	}

	tasks, err = s.RunningHookTasks(ctx, sessionID)
	if err != nil {
		t.Fatalf("RunningHookTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0] != (HookTask{
		PlanID: planID, TaskID: "task", WorkerID: workerID, Provider: "claude",
	}) {
		t.Fatalf("hook tasks = %+v", tasks)
	}
	if written, err := s.SetHookVerificationPending(ctx, sessionID, planID, "task"); err != nil || !written {
		t.Fatalf("SetHookVerificationPending: written=%v err=%v", written, err)
	}
	states, err := s.HookVerificationStates(ctx, sessionID)
	if err != nil || states[HookVerificationKey{PlanID: planID, TaskID: "task"}] != HookVerificationPending {
		t.Fatalf("pending states = %+v, err=%v", states, err)
	}
	if err := s.SetHookVerificationResult(ctx, sessionID, planID, "task", true); err != nil {
		t.Fatalf("SetHookVerificationResult: %v", err)
	}
	states, err = s.HookVerificationStates(ctx, sessionID)
	if err != nil || states[HookVerificationKey{PlanID: planID, TaskID: "task"}] != HookVerificationPassed {
		t.Fatalf("passed states = %+v, err=%v", states, err)
	}
	if err := s.InvalidateHookVerifications(ctx, sessionID); err != nil {
		t.Fatalf("InvalidateHookVerifications: %v", err)
	}
	states, err = s.HookVerificationStates(ctx, sessionID)
	if err != nil || len(states) != 0 {
		t.Fatalf("invalidated states = %+v, err=%v", states, err)
	}

	if err := s.ReleaseClaim(ctx, planID, "task", sessionID, "test"); err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}
	tasks, err = s.RunningHookTasks(ctx, sessionID)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("released hook tasks = %+v, err=%v", tasks, err)
	}
	if written, err := s.SetHookVerificationPending(ctx, sessionID, planID, "task"); err != nil || written {
		t.Fatalf("stale SetHookVerificationPending: written=%v err=%v", written, err)
	}
	states, err = s.HookVerificationStates(ctx, sessionID)
	if err != nil || len(states) != 0 {
		t.Fatalf("stale session created states = %+v, err=%v", states, err)
	}
}
