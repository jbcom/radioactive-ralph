package store

import (
	"context"
	"testing"
)

func TestCreateSessionRequiresRole(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if _, err := s.CreateSession(ctx, SessionOpts{}); err == nil {
		t.Error("CreateSession with empty Role: want error, got nil")
	}
}

func TestCreateSessionWithExplicitID(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	id, err := s.CreateSession(ctx, SessionOpts{ID: "explicit-session-id", Role: "supervisor", PID: 42, PIDStartTime: "t0", Host: "test-host"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if id != "explicit-session-id" {
		t.Errorf("CreateSession id = %q, want explicit-session-id", id)
	}
}

func TestHeartbeatSession(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	var before string
	if err := s.DB().QueryRowContext(ctx, "SELECT last_heartbeat FROM sessions WHERE id = ?", sessionID).Scan(&before); err != nil {
		t.Fatalf("read heartbeat before: %v", err)
	}

	if err := s.HeartbeatSession(ctx, sessionID); err != nil {
		t.Fatalf("HeartbeatSession: %v", err)
	}

	var after string
	if err := s.DB().QueryRowContext(ctx, "SELECT last_heartbeat FROM sessions WHERE id = ?", sessionID).Scan(&after); err != nil {
		t.Fatalf("read heartbeat after: %v", err)
	}
	if after == "" {
		t.Error("last_heartbeat empty after HeartbeatSession")
	}
}

func TestCloseSessionCascadesWorkers(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := s.CreateWorker(ctx, WorkerOpts{
		SessionID: sessionID, Provider: "claude", SubprocessPID: 100, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}

	if err := s.CloseSession(ctx, sessionID); err != nil {
		t.Fatalf("CloseSession: %v", err)
	}

	var sessionCount, workerCount int
	if err := s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions WHERE id = ?", sessionID).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if err := s.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM workers WHERE id = ?", workerID).Scan(&workerCount); err != nil {
		t.Fatalf("count workers: %v", err)
	}
	if sessionCount != 0 {
		t.Error("session still present after CloseSession")
	}
	if workerCount != 0 {
		t.Error("worker still present after CloseSession (want FK cascade delete)")
	}
}

func TestCloseSessionMissingIsNotAnError(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	if err := s.CloseSession(ctx, "does-not-exist"); err != nil {
		t.Errorf("CloseSession on missing session: want nil, got %v", err)
	}
}

func TestCreateWorkerRequiresFields(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	cases := []struct {
		name string
		opts WorkerOpts
	}{
		{"missing SessionID", WorkerOpts{Provider: "claude", SubprocessPID: 1, SubprocessStartTime: "t0"}},
		{"missing Provider", WorkerOpts{SessionID: sessionID, SubprocessPID: 1, SubprocessStartTime: "t0"}},
		{"zero SubprocessPID", WorkerOpts{SessionID: sessionID, Provider: "claude", SubprocessStartTime: "t0"}},
		{"missing SubprocessStartTime", WorkerOpts{SessionID: sessionID, Provider: "claude", SubprocessPID: 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := s.CreateWorker(ctx, c.opts); err == nil {
				t.Errorf("CreateWorker(%s): want error, got nil", c.name)
			}
		})
	}
}

func TestCreateWorkerWithNativeFanoutAndModel(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := s.CreateWorker(ctx, WorkerOpts{
		SessionID: sessionID, Provider: "opencode", Model: "sonnet",
		NativeFanout: true, SubprocessPID: 5, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}

	var provider, model string
	var fanout int
	if err := s.DB().QueryRowContext(ctx,
		"SELECT provider, COALESCE(model,''), native_fanout FROM workers WHERE id = ?", workerID,
	).Scan(&provider, &model, &fanout); err != nil {
		t.Fatalf("read worker row: %v", err)
	}
	if provider != "opencode" || model != "sonnet" || fanout != 1 {
		t.Errorf("worker row = provider=%q model=%q fanout=%d, want opencode/sonnet/1", provider, model, fanout)
	}
}

func TestSetWorkerTaskAndClearWorkerTask(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "worker-task-project")
	planID := mustCreatePlan(t, s, projectID, "worker-task-plan")
	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := s.CreateWorker(ctx, WorkerOpts{
		SessionID: sessionID, Provider: "claude", SubprocessPID: 1, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if err := s.CreateTask(ctx, CreateTaskOpts{PlanID: planID, ID: "task-x", Description: "d"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if err := s.SetWorkerTask(ctx, workerID, planID, "task-x"); err != nil {
		t.Fatalf("SetWorkerTask: %v", err)
	}
	var planCol, taskCol, status string
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COALESCE(current_plan_id,''), COALESCE(current_task_id,''), status FROM workers WHERE id = ?", workerID,
	).Scan(&planCol, &taskCol, &status); err != nil {
		t.Fatalf("read worker row: %v", err)
	}
	if planCol != planID || taskCol != "task-x" || status != "running" {
		t.Errorf("after SetWorkerTask: plan=%q task=%q status=%q, want %q/task-x/running", planCol, taskCol, status, planID)
	}

	if err := s.ClearWorkerTask(ctx, workerID, ""); err != nil {
		t.Fatalf("ClearWorkerTask: %v", err)
	}
	if err := s.DB().QueryRowContext(ctx,
		"SELECT COALESCE(current_plan_id,''), COALESCE(current_task_id,''), status FROM workers WHERE id = ?", workerID,
	).Scan(&planCol, &taskCol, &status); err != nil {
		t.Fatalf("read worker row after clear: %v", err)
	}
	if planCol != "" || taskCol != "" || status != "idle" {
		t.Errorf("after ClearWorkerTask(empty status): plan=%q task=%q status=%q, want empty/empty/idle", planCol, taskCol, status)
	}

	if err := s.ClearWorkerTask(ctx, workerID, "crashed"); err != nil {
		t.Fatalf("ClearWorkerTask(crashed): %v", err)
	}
	if err := s.DB().QueryRowContext(ctx, "SELECT status FROM workers WHERE id = ?", workerID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "crashed" {
		t.Errorf("status = %q, want crashed", status)
	}
}

func TestHeartbeatWorker(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	workerID, err := s.CreateWorker(ctx, WorkerOpts{
		SessionID: sessionID, Provider: "claude", SubprocessPID: 1, SubprocessStartTime: "t0",
	})
	if err != nil {
		t.Fatalf("CreateWorker: %v", err)
	}
	if err := s.HeartbeatWorker(ctx, workerID); err != nil {
		t.Fatalf("HeartbeatWorker: %v", err)
	}
}

func TestCountRunningWorkers(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	sessionID, err := s.CreateSession(ctx, SessionOpts{Role: "supervisor", PID: 1, PIDStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	n, err := s.CountRunningWorkers(ctx)
	if err != nil {
		t.Fatalf("CountRunningWorkers (none yet): %v", err)
	}
	if n != 0 {
		t.Fatalf("CountRunningWorkers = %d, want 0", n)
	}

	worker1, err := s.CreateWorker(ctx, WorkerOpts{SessionID: sessionID, Provider: "claude", SubprocessPID: 1, SubprocessStartTime: "t0"})
	if err != nil {
		t.Fatalf("CreateWorker 1: %v", err)
	}
	if _, err := s.CreateWorker(ctx, WorkerOpts{SessionID: sessionID, Provider: "claude", SubprocessPID: 2, SubprocessStartTime: "t0"}); err != nil {
		t.Fatalf("CreateWorker 2: %v", err)
	}

	n, err = s.CountRunningWorkers(ctx)
	if err != nil {
		t.Fatalf("CountRunningWorkers: %v", err)
	}
	if n != 2 {
		t.Fatalf("CountRunningWorkers = %d, want 2 (both start status=running)", n)
	}

	if err := s.ClearWorkerTask(ctx, worker1, "idle"); err != nil {
		t.Fatalf("ClearWorkerTask: %v", err)
	}
	n, err = s.CountRunningWorkers(ctx)
	if err != nil {
		t.Fatalf("CountRunningWorkers after clear: %v", err)
	}
	if n != 1 {
		t.Fatalf("CountRunningWorkers after clearing one = %d, want 1", n)
	}
}
