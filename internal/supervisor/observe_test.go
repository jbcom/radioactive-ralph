package supervisor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jonboulle/clockwork"
)

func TestHandleObserveSnapshotAndMessagesUseSafeService(t *testing.T) {
	ctx := context.Background()
	sup := newTestSupervisor(t, clockwork.NewRealClock())
	projectID, err := sup.store.CreateProject(
		ctx,
		"supervisor-observe",
		[]store.Fingerprint{{
			Kind:  store.FingerprintKindAbsPath,
			Value: "/private/repository/path-must-not-leak",
		}},
	)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	planID, err := sup.store.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "safe-plan",
		Title:          "Safe Plan",
		SourceMarkdown: "# secret source markdown",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := sup.store.CreateTask(ctx, store.CreateTaskOpts{
		PlanID:         planID,
		ID:             "task-1",
		Description:    "private task description",
		AcceptanceJSON: `["private acceptance command"]`,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := sup.store.AppendMessage(ctx, store.AppendMessageOpts{
		PlanID:      planID,
		TaskID:      "task-1",
		Role:        "ROLE_AGENT",
		ContentJSON: `{"provider_output":"private message output"}`,
	}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := sup.store.Emit(ctx, store.EmitOpts{
		ProjectID:   projectID,
		PlanID:      planID,
		TaskID:      "task-1",
		Kind:        "worker.dispatch_error",
		Stream:      "service",
		Actor:       "provider-session-secret",
		PayloadJSON: `{"reason":"private event payload"}`,
	}); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	snapshot, err := sup.HandleObserveSnapshot(ctx, ipc.ObserveSnapshotArgs{
		ProjectID:  projectID,
		PlanLimit:  10,
		TaskLimit:  10,
		EventLimit: 10,
	})
	if err != nil {
		t.Fatalf("HandleObserveSnapshot: %v", err)
	}
	if snapshot == nil || snapshot.Project.ID != projectID ||
		len(snapshot.Tasks.Items) != 1 ||
		snapshot.Tasks.Items[0].A2ATask == nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.RecentEvents.Items[0].Failure == nil ||
		snapshot.RecentEvents.Items[0].Failure.Category != observe.FailureDispatch {
		t.Fatalf("safe event failure = %+v", snapshot.RecentEvents.Items)
	}

	messages, err := sup.HandleObserveMessages(ctx, ipc.ObserveMessagesArgs{
		ProjectID: projectID,
		PlanID:    planID,
		TaskID:    "task-1",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("HandleObserveMessages: %v", err)
	}
	if messages == nil || len(messages.Items) != 1 ||
		messages.Items[0].CanonicalTaskID != planID+":task-1" {
		t.Fatalf("messages = %+v", messages)
	}

	encoded, err := json.Marshal(struct {
		Snapshot *ipc.ObserveSnapshotReply `json:"snapshot"`
		Messages *ipc.ObserveMessagesReply `json:"messages"`
	}{
		Snapshot: snapshot,
		Messages: messages,
	})
	if err != nil {
		t.Fatalf("marshal replies: %v", err)
	}
	for _, forbidden := range []string{
		"/private/repository/path-must-not-leak",
		"secret source markdown",
		"private task description",
		"private acceptance command",
		"private message output",
		"provider-session-secret",
		"private event payload",
		"source_markdown",
		"acceptance_json",
		"content_json",
		"payload_json",
		"actor",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("supervisor query leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestHandleObserveErrorsCarryStableIPCCodes(t *testing.T) {
	ctx := context.Background()
	sup := newTestSupervisor(t, clockwork.NewRealClock())

	if reply, err := sup.HandleObserveSnapshot(
		ctx,
		ipc.ObserveSnapshotArgs{ProjectID: "missing"},
	); reply != nil || !ipc.IsCode(err, ipc.CodeNotFound) {
		t.Fatalf("missing project = (%+v, %v), want nil not_found", reply, err)
	}
	if reply, err := sup.HandleObserveSnapshot(
		ctx,
		ipc.ObserveSnapshotArgs{ProjectID: ""},
	); reply != nil || !ipc.IsCode(err, ipc.CodeInvalidArgs) {
		t.Fatalf("invalid query = (%+v, %v), want nil invalid_args", reply, err)
	}
}

func TestAttachEventFrameDropsRawContentAndMatchesSnapshotTaxonomy(t *testing.T) {
	frame := attachEventFrame(store.Event{
		ID:          17,
		PlanID:      "plan-1",
		TaskID:      "task-1",
		Kind:        "worker.dispatch_panic",
		Stream:      "service",
		Actor:       "provider-session-secret",
		PayloadJSON: `{"reason":"raw panic output secret"}`,
		OccurredAt:  time.Date(2026, time.July, 26, 18, 30, 0, 0, time.UTC),
	})
	if frame.Failure == nil ||
		frame.Failure.Category != observe.FailureDispatch ||
		frame.Failure.Summary != "worker dispatch failed unexpectedly" {
		t.Fatalf("attach safe failure = %+v", frame.Failure)
	}
	encoded, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal attach frame: %v", err)
	}
	for _, forbidden := range []string{
		"provider-session-secret",
		"raw panic output secret",
		`"actor"`,
		`"payload"`,
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("attach frame JSON leaked %q: %s", forbidden, encoded)
		}
	}
}
