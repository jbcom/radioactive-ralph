package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func mustAppendOperatorMessage(
	t *testing.T,
	s *Store,
	workerID, planID, taskID, role, content string,
) int64 {
	t.Helper()
	ctx := context.Background()
	if err := s.AppendMessage(ctx, AppendMessageOpts{
		WorkerID:    workerID,
		PlanID:      planID,
		TaskID:      taskID,
		Role:        role,
		ContentJSON: content,
	}); err != nil {
		t.Fatalf("AppendMessage(%s/%s): %v", planID, taskID, err)
	}
	var id int64
	if err := s.DB().QueryRowContext(ctx, `SELECT MAX(id) FROM a2a_messages`).Scan(&id); err != nil {
		t.Fatalf("read appended message id: %v", err)
	}
	return id
}

func TestListOperatorMessagesIsolatedChronologicalAndContentFree(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectA := mustCreateProject(t, s, "operator-messages-a")
	projectB := mustCreateProject(t, s, "operator-messages-b")
	planA := mustCreatePlan(t, s, projectA, "messages-plan-a")
	planB := mustCreatePlan(t, s, projectB, "messages-plan-b")
	mustCreateOperatorTask(t, s, planA, "task-a")
	mustCreateOperatorTask(t, s, planA, "task-a-two")
	mustCreateOperatorTask(t, s, planB, "task-b")
	_, workerID := mustCreateSessionAndWorker(t, s, "operator-messages")

	const contentSecret = "/private/repository/raw-provider-message.json"
	messageIDs := make([]int64, 0, 3)
	messageIDs = append(messageIDs,
		mustAppendOperatorMessage(
			t,
			s,
			workerID,
			planA,
			"task-a",
			"ROLE_AGENT",
			`{"provider_output":"`+contentSecret+`","acceptance":"go test ./..."}`,
		),
	)
	mustAppendOperatorMessage(
		t,
		s,
		"",
		planB,
		"task-b",
		"ROLE_AGENT",
		`{"provider_output":"project-b-private"}`,
	)
	messageIDs = append(messageIDs,
		mustAppendOperatorMessage(
			t,
			s,
			"",
			planA,
			"task-a-two",
			"ROLE_USER",
			`{"text":"second"}`,
		),
		mustAppendOperatorMessage(
			t,
			s,
			workerID,
			planA,
			"task-a",
			"ROLE_AGENT",
			`{"text":"third"}`,
		),
	)

	first, err := s.ListOperatorMessages(ctx, OperatorMessageQuery{
		ProjectID: projectA,
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("first ListOperatorMessages: %v", err)
	}
	if len(first.Items) != 2 || !first.HasMore ||
		first.NextAfterID != messageIDs[1] {
		t.Fatalf("first page = %+v, want two rows and cursor %d", first, messageIDs[1])
	}
	if first.Items[0].ID != messageIDs[0] || first.Items[1].ID != messageIDs[1] {
		t.Fatalf("first page ids = %+v, want chronological %v", first.Items, messageIDs[:2])
	}
	second, err := s.ListOperatorMessages(ctx, OperatorMessageQuery{
		ProjectID: projectA,
		AfterID:   first.NextAfterID,
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("second ListOperatorMessages: %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].ID != messageIDs[2] ||
		second.HasMore || second.NextAfterID != 0 {
		t.Fatalf("second page = %+v, want final chronological row", second)
	}
	if first.Items[0].WorkerID != workerID {
		t.Fatalf("worker control id = %q, want %q", first.Items[0].WorkerID, workerID)
	}
	for _, item := range append(first.Items, second.Items...) {
		if item.PlanID == planB || item.TaskID == "task-b" {
			t.Fatalf("project B message leaked into project A page: %+v", item)
		}
	}

	filtered, err := s.ListOperatorMessages(ctx, OperatorMessageQuery{
		ProjectID: projectA,
		PlanID:    planA,
		TaskID:    "task-a",
	})
	if err != nil {
		t.Fatalf("filtered ListOperatorMessages: %v", err)
	}
	if len(filtered.Items) != 2 ||
		filtered.Items[0].ID != messageIDs[0] ||
		filtered.Items[1].ID != messageIDs[2] {
		t.Fatalf("filtered task messages = %+v, want first and third", filtered.Items)
	}

	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal operator message page: %v", err)
	}
	for _, forbidden := range []string{
		contentSecret,
		"project-b-private",
		"provider_output",
		"content_json",
		"acceptance",
		"session_id",
		"provider_session_id",
		"subprocess_pid",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("message metadata JSON leaked forbidden value or field %q: %s", forbidden, encoded)
		}
	}
}

func TestListOperatorMessagesValidatesScopeCursorAndBounds(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectA := mustCreateProject(t, s, "operator-message-validation-a")
	projectB := mustCreateProject(t, s, "operator-message-validation-b")
	planA := mustCreatePlan(t, s, projectA, "message-validation-plan-a")
	planB := mustCreatePlan(t, s, projectB, "message-validation-plan-b")
	mustCreateOperatorTask(t, s, planA, "task-a")
	mustCreateOperatorTask(t, s, planB, "task-b")
	mustAppendOperatorMessage(t, s, "", planA, "task-a", "ROLE_AGENT", `{}`)
	foreignMessageID := mustAppendOperatorMessage(
		t,
		s,
		"",
		planB,
		"task-b",
		"ROLE_AGENT",
		`{}`,
	)

	invalidQueries := map[string]OperatorMessageQuery{
		"missing project": {},
		"task without plan": {
			ProjectID: projectA,
			TaskID:    "task-a",
		},
		"negative cursor": {
			ProjectID: projectA,
			AfterID:   -1,
		},
		"negative limit": {
			ProjectID: projectA,
			Limit:     -1,
		},
		"limit above max": {
			ProjectID: projectA,
			Limit:     MaxOperatorMessageLimit + 1,
		},
	}
	for name, query := range invalidQueries {
		t.Run(name, func(t *testing.T) {
			got, err := s.ListOperatorMessages(ctx, query)
			if got != nil || !errors.Is(err, ErrOperatorInvalidQuery) {
				t.Fatalf("result = (%+v, %v), want nil ErrOperatorInvalidQuery", got, err)
			}
		})
	}
	for name, query := range map[string]OperatorMessageQuery{
		"unknown project": {ProjectID: "missing"},
		"foreign plan": {
			ProjectID: projectA,
			PlanID:    planB,
		},
		"foreign task": {
			ProjectID: projectA,
			PlanID:    planA,
			TaskID:    "task-b",
		},
		"foreign cursor": {
			ProjectID: projectA,
			AfterID:   foreignMessageID,
		},
		"cursor outside task filter": {
			ProjectID: projectA,
			PlanID:    planA,
			TaskID:    "task-a",
			AfterID:   foreignMessageID,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := s.ListOperatorMessages(ctx, query)
			switch name {
			case "unknown project":
				if got != nil || !errors.Is(err, ErrOperatorProjectNotFound) {
					t.Fatalf("result = (%+v, %v), want nil ErrOperatorProjectNotFound", got, err)
				}
			case "foreign plan":
				if got != nil || !errors.Is(err, ErrOperatorPlanNotFound) {
					t.Fatalf("result = (%+v, %v), want nil ErrOperatorPlanNotFound", got, err)
				}
			case "foreign task":
				if got != nil || !errors.Is(err, ErrOperatorTaskNotFound) {
					t.Fatalf("result = (%+v, %v), want nil ErrOperatorTaskNotFound", got, err)
				}
			default:
				if got != nil || !errors.Is(err, ErrOperatorInvalidCursor) {
					t.Fatalf("result = (%+v, %v), want nil ErrOperatorInvalidCursor", got, err)
				}
			}
		})
	}
}

func TestListOperatorMessagesQueryErrorReturnsNoEmptyPage(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "operator-message-query-error")
	if _, err := s.DB().ExecContext(ctx, `DROP TABLE a2a_messages`); err != nil {
		t.Fatalf("drop a2a_messages: %v", err)
	}

	got, err := s.ListOperatorMessages(ctx, OperatorMessageQuery{ProjectID: projectID})
	if err == nil {
		t.Fatal("ListOperatorMessages succeeded after messages table was dropped")
	}
	if got != nil {
		t.Fatalf("ListOperatorMessages returned plausible empty page on error: %+v", got)
	}
}
