package orch

import (
	"context"
	"runtime"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

func TestCanStopAsRechecksAcceptanceWithoutCompletingTask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX acceptance command")
	}
	for _, tc := range []struct {
		name       string
		acceptance string
		want       bool
	}{
		{name: "passing mechanical", acceptance: `{"command":"exit 0"}`, want: true},
		{name: "failing mechanical", acceptance: `{"command":"exit 1"}`, want: false},
		{name: "judgment-only has no independent predicate", acceptance: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := newTestStore(t)
			projectID := mustCreateTestProject(t, s, "hook-stop-project")
			planID := mustCreateTestPlan(t, s, projectID, "hook-stop-plan", "Hook", "# Hook\n\n- task\n")
			if err := s.CreateTask(ctx, store.CreateTaskOpts{
				PlanID: planID, ID: "task", Description: "task", AcceptanceJSON: tc.acceptance,
			}); err != nil {
				t.Fatalf("CreateTask: %v", err)
			}
			sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
			if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
				t.Fatalf("ClaimNextReady: %v", err)
			}

			got, err := New(s).CanStopAs(ctx, planID, "task", sessionID)
			if err != nil {
				t.Fatalf("CanStopAs: %v", err)
			}
			if got != tc.want {
				t.Fatalf("CanStopAs = %v, want %v", got, tc.want)
			}
			task, err := s.GetTask(ctx, planID, "task")
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			if task.Status != store.TaskStatusRunning {
				t.Fatalf("hook verification mutated task status to %q", task.Status)
			}
			if stale, err := New(s).CanStopAs(ctx, planID, "task", "stale-session"); err != nil || stale {
				t.Fatalf("stale CanStopAs = %v, err=%v", stale, err)
			}
		})
	}
}
