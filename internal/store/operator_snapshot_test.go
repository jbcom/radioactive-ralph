package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

func mustCreateOperatorTask(t *testing.T, s *Store, planID, taskID string) {
	t.Helper()
	err := s.CreateTask(context.Background(), CreateTaskOpts{
		PlanID:         planID,
		ID:             taskID,
		Description:    "operator test task " + taskID,
		AcceptanceJSON: `["go test ./..."]`,
	})
	if err != nil {
		t.Fatalf("CreateTask(%s/%s): %v", planID, taskID, err)
	}
}

func mustEmitOperatorEvent(
	t *testing.T,
	s *Store,
	projectID, planID, taskID, kind, payload string,
) int64 {
	t.Helper()
	ctx := context.Background()
	if err := s.Emit(ctx, EmitOpts{
		ProjectID:   projectID,
		PlanID:      planID,
		TaskID:      taskID,
		Kind:        kind,
		Stream:      "worker",
		Actor:       "provider-session-secret",
		PayloadJSON: payload,
	}); err != nil {
		t.Fatalf("Emit(%s): %v", kind, err)
	}
	var id int64
	if err := s.DB().QueryRowContext(ctx, `SELECT MAX(id) FROM events`).Scan(&id); err != nil {
		t.Fatalf("read emitted event id: %v", err)
	}
	return id
}

func TestReadOperatorSnapshotProjectIsolationFanoutAndContentSafety(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)

	projectA := mustCreateProject(t, s, "operator-project-a")
	projectB := mustCreateProject(t, s, "operator-project-b")
	planA := mustCreatePlan(t, s, projectA, "plan-a")
	planB := mustCreatePlan(t, s, projectB, "plan-b")
	mustCreateOperatorTask(t, s, planA, "a-one")
	mustCreateOperatorTask(t, s, planA, "a-two")
	mustCreateOperatorTask(t, s, planB, "b-one")

	const (
		sourceSecret     = "/private/repository/source-plan.md"
		acceptanceSecret = "rm -rf /private/operator-safety-secret"
		payloadSecret    = "raw-provider-output-must-not-leak"
	)
	if _, err := s.DB().ExecContext(ctx, `
		UPDATE plans SET source_markdown = ? WHERE id = ?
	`, sourceSecret, planA); err != nil {
		t.Fatalf("seed source markdown: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `
		UPDATE tasks SET acceptance_json = ?, description = ?
		WHERE plan_id = ? AND id = ?
	`, `["`+acceptanceSecret+`"]`, sourceSecret, planA, "a-one"); err != nil {
		t.Fatalf("seed task private content: %v", err)
	}

	sessionA, workerA := mustCreateSessionAndWorker(t, s, "operator-a")
	if _, err := s.DB().ExecContext(ctx, `
		UPDATE workers
		SET provider = 'codex', model = 'gpt-5', native_fanout = 1
		WHERE id = ?
	`, workerA); err != nil {
		t.Fatalf("configure fanout worker: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, claimed_by_session = ?, claimed_by_worker_id = ?
		WHERE plan_id = ? AND id IN ('a-one', 'a-two')
	`, string(TaskStatusRunning), sessionA, workerA, planA); err != nil {
		t.Fatalf("seed fanout claims: %v", err)
	}
	if err := s.SetWorkerTask(ctx, workerA, planA, "a-one"); err != nil {
		t.Fatalf("SetWorkerTask(A): %v", err)
	}

	sessionB, workerB := mustCreateSessionAndWorker(t, s, "operator-b")
	if _, err := s.DB().ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, claimed_by_session = ?, claimed_by_worker_id = ?
		WHERE plan_id = ? AND id = 'b-one'
	`, string(TaskStatusRunning), sessionB, workerB, planB); err != nil {
		t.Fatalf("seed project B claim: %v", err)
	}
	if err := s.SetWorkerTask(ctx, workerB, planB, "b-one"); err != nil {
		t.Fatalf("SetWorkerTask(B): %v", err)
	}

	mustEmitOperatorEvent(
		t,
		s,
		projectA,
		planA,
		"a-one",
		"worker.private-output",
		`{"output":"`+payloadSecret+`","provider_session_id":"hidden-session"}`,
	)
	mustEmitOperatorEvent(
		t,
		s,
		projectB,
		planB,
		"b-one",
		"worker.project-b",
		`{"output":"project-b-only"}`,
	)
	// A contradictory event belongs to its explicit project. Its foreign
	// plan/task references must be scrubbed before the A snapshot leaves the
	// store boundary.
	mustEmitOperatorEvent(
		t,
		s,
		projectA,
		planB,
		"b-one",
		"worker.contradictory-scope",
		`{"output":"also-private"}`,
	)

	got, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{ProjectID: projectA})
	if err != nil {
		t.Fatalf("ReadOperatorSnapshot: %v", err)
	}
	if got.Project.ID != projectA {
		t.Fatalf("project id = %q, want %q", got.Project.ID, projectA)
	}
	if got.ActiveWorkerCount != 1 || len(got.Workers) != 1 {
		t.Fatalf(
			"workers = (%d, %+v), want one complete active worker",
			got.ActiveWorkerCount,
			got.Workers,
		)
	}
	worker := got.Workers[0]
	if worker.ID != workerA || worker.Provider != "codex" || !worker.NativeFanout {
		t.Fatalf("worker = %+v, want project A fanout worker", worker)
	}
	if len(worker.Claims) != 2 {
		t.Fatalf("worker claims = %+v, want both native fan-out claims", worker.Claims)
	}
	if worker.Claims[0].TaskID != "a-one" || worker.Claims[1].TaskID != "a-two" {
		t.Errorf("worker claims order = %+v, want a-one, a-two", worker.Claims)
	}
	for _, plan := range got.Plans.Items {
		if plan.ID == planB {
			t.Fatalf("project B plan leaked into A snapshot: %+v", plan)
		}
	}
	for _, task := range got.Tasks.Items {
		if task.PlanID == planB {
			t.Fatalf("project B task leaked into A snapshot: %+v", task)
		}
	}
	for _, event := range got.RecentEvents.Items {
		if event.Kind == "worker.project-b" {
			t.Fatalf("project B event leaked into A snapshot: %+v", event)
		}
		if event.Kind == "worker.contradictory-scope" &&
			(event.PlanID != "" || event.TaskID != "") {
			t.Fatalf("contradictory foreign scope was not scrubbed: %+v", event)
		}
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	for _, forbidden := range []string{
		sourceSecret,
		acceptanceSecret,
		payloadSecret,
		"project-b-only",
		sessionA,
		sessionB,
		workerB,
		"source_markdown",
		"acceptance_json",
		"description",
		"payload_json",
		"actor",
		"provider_session_id",
		"subprocess_pid",
		"session_id",
		"fingerprint",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("snapshot JSON leaked forbidden value or field %q: %s", forbidden, encoded)
		}
	}
	if got.PlanCounts == nil || got.TaskCounts == nil ||
		got.Plans.Items == nil || got.Tasks.Items == nil ||
		got.Workers == nil || got.RecentEvents.Items == nil {
		t.Fatal("successful snapshot must use non-nil collections")
	}
	for i := 1; i < len(got.TaskCounts); i++ {
		if got.TaskCounts[i-1].Status > got.TaskCounts[i].Status {
			t.Fatalf("task status counts are not deterministic: %+v", got.TaskCounts)
		}
	}
}

func TestReadOperatorSnapshotPaginatesDeterministically(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "operator-pagination")

	planIDs := []string{
		mustCreatePlan(t, s, projectID, "plan-z"),
		mustCreatePlan(t, s, projectID, "plan-a"),
		mustCreatePlan(t, s, projectID, "plan-m"),
	}
	for _, planID := range planIDs {
		mustCreateOperatorTask(t, s, planID, "task-z")
		mustCreateOperatorTask(t, s, planID, "task-a")
	}
	sort.Strings(planIDs)

	eventIDs := []int64{
		mustEmitOperatorEvent(t, s, projectID, "", "", "event-one", `{}`),
		mustEmitOperatorEvent(t, s, projectID, "", "", "event-two", `{}`),
		mustEmitOperatorEvent(t, s, projectID, "", "", "event-three", `{}`),
	}

	first, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{
		ProjectID:  projectID,
		PlanLimit:  2,
		TaskLimit:  2,
		EventLimit: 2,
	})
	if err != nil {
		t.Fatalf("first ReadOperatorSnapshot: %v", err)
	}
	if !first.Plans.HasMore || first.Plans.NextAfterID == "" {
		t.Fatalf("first plan page lacks cursor: %+v", first.Plans)
	}
	if got := []string{first.Plans.Items[0].ID, first.Plans.Items[1].ID}; got[0] != planIDs[0] || got[1] != planIDs[1] {
		t.Fatalf("first plan ids = %v, want %v", got, planIDs[:2])
	}
	if !first.Tasks.HasMore || first.Tasks.NextAfter == nil {
		t.Fatalf("first task page lacks cursor: %+v", first.Tasks)
	}
	if !first.RecentEvents.HasMore ||
		first.RecentEvents.NextBeforeID != eventIDs[1] {
		t.Fatalf("first event page/cursor = %+v, want newest two", first.RecentEvents)
	}
	if first.RecentEvents.Items[0].ID != eventIDs[2] ||
		first.RecentEvents.Items[1].ID != eventIDs[1] {
		t.Fatalf("first events = %+v, want newest-first", first.RecentEvents.Items)
	}

	second, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{
		ProjectID:     projectID,
		PlanLimit:     2,
		PlanAfterID:   first.Plans.NextAfterID,
		TaskLimit:     2,
		TaskAfter:     *first.Tasks.NextAfter,
		EventLimit:    2,
		EventBeforeID: first.RecentEvents.NextBeforeID,
	})
	if err != nil {
		t.Fatalf("second ReadOperatorSnapshot: %v", err)
	}
	if len(second.Plans.Items) != 1 || second.Plans.Items[0].ID != planIDs[2] {
		t.Fatalf("second plans = %+v, want final plan", second.Plans)
	}
	if len(second.Tasks.Items) != 2 {
		t.Fatalf("second tasks = %+v, want next two tasks", second.Tasks)
	}
	firstTaskCursor := *first.Tasks.NextAfter
	for _, task := range second.Tasks.Items {
		if task.PlanID < firstTaskCursor.PlanID ||
			(task.PlanID == firstTaskCursor.PlanID && task.ID <= firstTaskCursor.TaskID) {
			t.Fatalf("task cursor repeated or reversed a row: cursor=%+v task=%+v", firstTaskCursor, task)
		}
	}
	if len(second.RecentEvents.Items) != 1 ||
		second.RecentEvents.Items[0].ID != eventIDs[0] {
		t.Fatalf("second events = %+v, want oldest event", second.RecentEvents)
	}

	foreignProject := mustCreateProject(t, s, "operator-pagination-foreign")
	foreignPlan := mustCreatePlan(t, s, foreignProject, "foreign-plan")
	mustCreateOperatorTask(t, s, foreignPlan, "foreign-task")
	foreignEvent := mustEmitOperatorEvent(t, s, foreignProject, "", "", "foreign-event", `{}`)
	for name, query := range map[string]OperatorSnapshotQuery{
		"plan": {
			ProjectID:   projectID,
			PlanAfterID: foreignPlan,
		},
		"task": {
			ProjectID: projectID,
			TaskAfter: OperatorTaskCursor{PlanID: foreignPlan, TaskID: "foreign-task"},
		},
		"event": {
			ProjectID:     projectID,
			EventBeforeID: foreignEvent,
		},
	} {
		t.Run("foreign "+name+" cursor", func(t *testing.T) {
			got, err := s.ReadOperatorSnapshot(ctx, query)
			if got != nil || !errors.Is(err, ErrOperatorInvalidCursor) {
				t.Fatalf("result = (%+v, %v), want nil ErrOperatorInvalidCursor", got, err)
			}
		})
	}
}

func TestReadOperatorSnapshotUsesOneConsistentReadTransaction(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "operator-consistency")
	planID := mustCreatePlan(t, s, projectID, "consistent-plan")
	if _, err := s.DB().ExecContext(ctx, `UPDATE plans SET title = 'before' WHERE id = ?`, planID); err != nil {
		t.Fatalf("seed plan title: %v", err)
	}

	got, err := s.readOperatorSnapshot(
		ctx,
		OperatorSnapshotQuery{ProjectID: projectID},
		&operatorSnapshotHooks{
			afterProjectRead: func() error {
				_, updateErr := s.DB().ExecContext(
					ctx,
					`UPDATE plans SET title = 'after' WHERE id = ?`,
					planID,
				)
				return updateErr
			},
		},
	)
	if err != nil {
		t.Fatalf("readOperatorSnapshot: %v", err)
	}
	if len(got.Plans.Items) != 1 || got.Plans.Items[0].Title != "before" {
		t.Fatalf("snapshot plans = %+v, want pre-write title from one read snapshot", got.Plans.Items)
	}
	after, err := s.GetPlan(ctx, planID)
	if err != nil {
		t.Fatalf("GetPlan after concurrent write: %v", err)
	}
	if after.Title != "after" {
		t.Fatalf("committed plan title = %q, want after", after.Title)
	}
}

func TestReadOperatorSnapshotValidatesInputAndHardBounds(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "operator-bounds")

	tests := map[string]OperatorSnapshotQuery{
		"missing project": {},
		"negative plan limit": {
			ProjectID: projectID,
			PlanLimit: -1,
		},
		"plan limit above max": {
			ProjectID: projectID,
			PlanLimit: MaxOperatorPageLimit + 1,
		},
		"negative task limit": {
			ProjectID: projectID,
			TaskLimit: -1,
		},
		"task limit above max": {
			ProjectID: projectID,
			TaskLimit: MaxOperatorPageLimit + 1,
		},
		"negative event limit": {
			ProjectID:  projectID,
			EventLimit: -1,
		},
		"event limit above max": {
			ProjectID:  projectID,
			EventLimit: MaxOperatorEventLimit + 1,
		},
		"negative event cursor": {
			ProjectID:     projectID,
			EventBeforeID: -1,
		},
		"partial task cursor plan": {
			ProjectID: projectID,
			TaskAfter: OperatorTaskCursor{PlanID: "plan-only"},
		},
		"partial task cursor task": {
			ProjectID: projectID,
			TaskAfter: OperatorTaskCursor{TaskID: "task-only"},
		},
	}
	for name, query := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := s.ReadOperatorSnapshot(ctx, query)
			if got != nil || !errors.Is(err, ErrOperatorInvalidQuery) {
				t.Fatalf("result = (%+v, %v), want nil ErrOperatorInvalidQuery", got, err)
			}
		})
	}
	t.Run("unknown project", func(t *testing.T) {
		got, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{ProjectID: "missing"})
		if got != nil || !errors.Is(err, ErrOperatorProjectNotFound) {
			t.Fatalf("result = (%+v, %v), want nil ErrOperatorProjectNotFound", got, err)
		}
	})

	planID := mustCreatePlan(t, s, projectID, "worker-cap-plan")
	sessionID, err := s.CreateSession(ctx, SessionOpts{
		Role:         "supervisor",
		PID:          1,
		PIDStartTime: "operator-hard-bound",
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	tx, err := s.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin worker seed: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < MaxOperatorActiveWorkers+1; i++ {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO workers(
				id, session_id, provider, native_fanout,
				subprocess_pid, subprocess_start_time,
				current_plan_id, started_at, last_heartbeat, status
			) VALUES (?, ?, 'codex', 0, ?, ?, ?, ?, ?, 'running')
		`,
			fmt.Sprintf("operator-worker-%03d", i),
			sessionID,
			i+1,
			fmt.Sprintf("worker-start-%03d", i),
			planID,
			now,
			now,
		)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("seed worker %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit worker seed: %v", err)
	}
	got, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{ProjectID: projectID})
	if got != nil || !errors.Is(err, ErrOperatorSnapshotTooLarge) {
		t.Fatalf("hard-bound result = (%+v, %v), want nil ErrOperatorSnapshotTooLarge", got, err)
	}
}

func TestReadOperatorSnapshotLateQueryErrorReturnsNoPartialSnapshot(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "operator-query-error")
	planID := mustCreatePlan(t, s, projectID, "query-error-plan")
	mustCreateOperatorTask(t, s, planID, "running-task")
	sessionID, workerID := mustCreateSessionAndWorker(t, s, "query-error")
	if _, err := s.DB().ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, claimed_by_session = ?, claimed_by_worker_id = ?
		WHERE plan_id = ? AND id = 'running-task'
	`, string(TaskStatusRunning), sessionID, workerID, planID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}
	if err := s.SetWorkerTask(ctx, workerID, planID, "running-task"); err != nil {
		t.Fatalf("SetWorkerTask: %v", err)
	}
	if _, err := s.DB().ExecContext(ctx, `DROP TABLE events`); err != nil {
		t.Fatalf("drop events: %v", err)
	}

	got, err := s.ReadOperatorSnapshot(ctx, OperatorSnapshotQuery{ProjectID: projectID})
	if err == nil {
		t.Fatal("ReadOperatorSnapshot succeeded after late query table was dropped")
	}
	if got != nil {
		t.Fatalf(
			"ReadOperatorSnapshot returned partial state on error: %+v; want nil so zero is never plausible",
			got,
		)
	}
}
