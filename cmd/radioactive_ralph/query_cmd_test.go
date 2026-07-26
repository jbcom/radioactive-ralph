package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
)

type fakeObserveClient struct {
	snapshot    *ipc.ObserveSnapshotReply
	snapshotErr error
	snapshotQ   ipc.ObserveSnapshotArgs
	messages    *ipc.ObserveMessagesReply
	messagesErr error
	messagesQ   ipc.ObserveMessagesArgs
	attach      []ipc.AttachEvent
	attachErr   error
	attachArgs  ipc.AttachArgs
}

func (f *fakeObserveClient) ObserveSnapshot(
	_ context.Context,
	args ipc.ObserveSnapshotArgs,
) (*ipc.ObserveSnapshotReply, error) {
	f.snapshotQ = args
	return f.snapshot, f.snapshotErr
}

func (f *fakeObserveClient) ObserveMessages(
	_ context.Context,
	args ipc.ObserveMessagesArgs,
) (*ipc.ObserveMessagesReply, error) {
	f.messagesQ = args
	return f.messages, f.messagesErr
}

func (f *fakeObserveClient) AttachEvents(
	_ context.Context,
	args ipc.AttachArgs,
	fn func(ipc.AttachEvent) error,
) error {
	f.attachArgs = args
	for _, event := range f.attach {
		if err := fn(event); err != nil {
			return err
		}
	}
	return f.attachErr
}

func (f *fakeObserveClient) Close() error { return nil }

func querySnapshotFixture(activeWorkers int) *ipc.ObserveSnapshotReply {
	workers := make([]observe.Worker, activeWorkers)
	return &ipc.ObserveSnapshotReply{
		SchemaVersion: observe.SchemaVersion,
		CapturedAt: time.Date(
			2026,
			time.July,
			26,
			19,
			0,
			0,
			0,
			time.UTC,
		),
		Project: observe.Project{ID: "project-1"},
		Summary: observe.Summary{
			PlanTotal:         2,
			TaskTotal:         5,
			ActiveWorkerCount: activeWorkers,
			ZeroActiveWorkers: activeWorkers == 0,
			PlanStatusCounts:  []observe.StatusCount{},
			TaskStatusCounts:  []observe.StatusCount{},
		},
		Plans:        observe.PlanPage{Items: []observe.Plan{}},
		Tasks:        observe.TaskPage{Items: []observe.Task{}},
		Workers:      workers,
		RecentEvents: observe.EventPage{Items: []observe.Event{}},
	}
}

func TestRunStatusQueryWithInconsistentReplyFailsBeforeOutput(t *testing.T) {
	for _, mutate := range []func(*ipc.ObserveSnapshotReply){
		func(reply *ipc.ObserveSnapshotReply) { reply.SchemaVersion++ },
		func(reply *ipc.ObserveSnapshotReply) { reply.Project.ID = "other-project" },
		func(reply *ipc.ObserveSnapshotReply) { reply.Workers = nil },
		func(reply *ipc.ObserveSnapshotReply) {
			reply.Summary.ZeroActiveWorkers = true
		},
	} {
		reply := querySnapshotFixture(1)
		mutate(reply)
		client := &fakeObserveClient{snapshot: reply}
		var out bytes.Buffer
		err := runStatusQueryWith(
			context.Background(),
			&out,
			client,
			ipc.ObserveSnapshotArgs{ProjectID: "project-1"},
			true,
			true,
		)
		if err == nil {
			t.Fatal("inconsistent snapshot error = nil")
		}
		if out.Len() != 0 {
			t.Fatalf("inconsistent snapshot printed plausible output: %q", out.String())
		}
	}
}

func TestRunStatusQueryWithJSONAndZeroWorkerGate(t *testing.T) {
	query := ipc.ObserveSnapshotArgs{
		ProjectID:  "project-1",
		PlanLimit:  4,
		TaskLimit:  5,
		EventLimit: 6,
	}
	for _, test := range []struct {
		name          string
		activeWorkers int
		wantErr       bool
	}{
		{name: "zero", activeWorkers: 0},
		{name: "active", activeWorkers: 2, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeObserveClient{
				snapshot: querySnapshotFixture(test.activeWorkers),
			}
			var out bytes.Buffer
			err := runStatusQueryWith(
				context.Background(),
				&out,
				client,
				query,
				true,
				true,
			)
			if test.wantErr != errors.Is(err, errActiveWorkers) {
				t.Fatalf("error = %v, want active-worker gate=%v", err, test.wantErr)
			}
			if client.snapshotQ != query {
				t.Fatalf("snapshot query = %+v, want %+v", client.snapshotQ, query)
			}
			var decoded ipc.ObserveSnapshotReply
			if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
				t.Fatalf("status JSON = %q: %v", out.String(), err)
			}
			if decoded.Project.ID != "project-1" ||
				decoded.Summary.ActiveWorkerCount != test.activeWorkers {
				t.Fatalf("decoded status = %+v", decoded)
			}
		})
	}
}

func TestRunStatusQueryWithReadErrorPrintsNoPlausibleZero(t *testing.T) {
	injected := errors.New("query failed")
	client := &fakeObserveClient{snapshotErr: injected}
	var out bytes.Buffer
	err := runStatusQueryWith(
		context.Background(),
		&out,
		client,
		ipc.ObserveSnapshotArgs{ProjectID: "project-1"},
		true,
		true,
	)
	if !errors.Is(err, injected) {
		t.Fatalf("error = %v, want injected", err)
	}
	if out.Len() != 0 {
		t.Fatalf("query error printed plausible JSON: %q", out.String())
	}
}

func TestRunStatusQueryWithHumanSummary(t *testing.T) {
	client := &fakeObserveClient{snapshot: querySnapshotFixture(0)}
	var out bytes.Buffer
	if err := runStatusQueryWith(
		context.Background(),
		&out,
		client,
		ipc.ObserveSnapshotArgs{ProjectID: "project-1"},
		false,
		false,
	); err != nil {
		t.Fatalf("runStatusQueryWith: %v", err)
	}
	for _, want := range []string{
		"project=project-1",
		"plans=2",
		"tasks=5",
		"active_workers=0",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("human status %q missing %q", out.String(), want)
		}
	}
}

func TestRunMessagesQueryWithJSON(t *testing.T) {
	page := &ipc.ObserveMessagesReply{
		SchemaVersion: observe.SchemaVersion,
		Items: []observe.MessageMetadata{{
			ID:              3,
			PlanID:          "plan-1",
			TaskID:          "task-1",
			CanonicalTaskID: "plan-1:task-1",
			ContextID:       "plan-1",
			Role:            "ROLE_AGENT",
		}},
		HasMore:     true,
		NextAfterID: 3,
	}
	client := &fakeObserveClient{messages: page}
	query := ipc.ObserveMessagesArgs{
		ProjectID: "project-1",
		PlanID:    "plan-1",
		TaskID:    "task-1",
		Limit:     1,
	}
	var out bytes.Buffer
	if err := runMessagesQueryWith(
		context.Background(),
		&out,
		client,
		query,
	); err != nil {
		t.Fatalf("runMessagesQueryWith: %v", err)
	}
	if client.messagesQ != query {
		t.Fatalf("messages query = %+v, want %+v", client.messagesQ, query)
	}
	var decoded ipc.ObserveMessagesReply
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("messages JSON = %q: %v", out.String(), err)
	}
	if len(decoded.Items) != 1 ||
		decoded.Items[0].CanonicalTaskID != "plan-1:task-1" {
		t.Fatalf("decoded messages = %+v", decoded)
	}
	for _, forbidden := range []string{
		"content_json",
		"provider_output",
		"parts",
	} {
		if strings.Contains(out.String(), forbidden) {
			t.Errorf("messages JSON leaked %q: %s", forbidden, out.String())
		}
	}
}

func TestRunMessagesQueryWithSchemaMismatchPrintsNothing(t *testing.T) {
	client := &fakeObserveClient{messages: &ipc.ObserveMessagesReply{
		SchemaVersion: observe.SchemaVersion + 1,
	}}
	var out bytes.Buffer
	err := runMessagesQueryWith(
		context.Background(),
		&out,
		client,
		ipc.ObserveMessagesArgs{ProjectID: "project-1"},
	)
	if err == nil {
		t.Fatal("schema mismatch error = nil")
	}
	if out.Len() != 0 {
		t.Fatalf("schema mismatch printed output: %q", out.String())
	}
}

func TestQueryCommandErrorRequiresSupervisorUpgrade(t *testing.T) {
	err := queryCommandError(&ipc.CodedError{
		Class:   ipc.CodeUnsupportedCommand,
		Message: "unknown command",
	})
	if err == nil ||
		!strings.Contains(err.Error(), "upgrade and restart") ||
		!strings.Contains(err.Error(), "protocol v3") {
		t.Fatalf("upgrade error = %v", err)
	}
}

func TestQueryCommandsRegisteredWithAutomationFlags(t *testing.T) {
	root := newTestRootCmd(context.Background())
	status, _, err := root.Find([]string{"status"})
	if err != nil || status == nil {
		t.Fatalf("find status: (%+v, %v)", status, err)
	}
	for _, flag := range []string{
		"json",
		"require-zero-workers",
		"plan-limit",
		"plan-after",
		"task-limit",
		"task-after-plan",
		"task-after",
		"event-limit",
		"event-before",
	} {
		if status.Flags().Lookup(flag) == nil {
			t.Errorf("status command missing --%s", flag)
		}
	}
	messages, _, err := root.Find([]string{"messages"})
	if err != nil || messages == nil {
		t.Fatalf("find messages: (%+v, %v)", messages, err)
	}
	for _, flag := range []string{"plan", "task", "after", "limit"} {
		if messages.Flags().Lookup(flag) == nil {
			t.Errorf("messages command missing --%s", flag)
		}
	}
}
