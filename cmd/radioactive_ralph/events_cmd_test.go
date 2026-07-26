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
	"github.com/spf13/cobra"
)

// fakeEventSource scripts a backlog + a live stream for runEventsWith.
type fakeEventSource struct {
	backlog    []ipc.AttachEvent
	maxID      int64
	live       []ipc.AttachEvent
	attachErr  error
	gotAfterID int64 // records the cursor the live attach was called with
}

func (f *fakeEventSource) Backlog(_ context.Context, _ string, n int) ([]ipc.AttachEvent, int64, error) {
	if n <= 0 {
		return nil, f.maxID, nil
	}
	return f.backlog, f.maxID, nil
}

func (f *fakeEventSource) AttachEvents(_ context.Context, args ipc.AttachArgs, fn func(ipc.AttachEvent) error) error {
	f.gotAfterID = args.AfterID
	for _, ev := range f.live {
		if err := fn(ev); err != nil {
			return err
		}
	}
	return f.attachErr
}

func TestRunEventsWith_BacklogThenLive(t *testing.T) {
	at := time.Date(2026, 7, 17, 8, 30, 0, 0, time.UTC)
	f := &fakeEventSource{
		backlog: []ipc.AttachEvent{
			{ID: 4, Kind: "task.claimed", TaskID: "t1", OccurredAt: at},
			{ID: 5, Kind: "task.done", TaskID: "t1", OccurredAt: at},
		},
		maxID: 5,
		live: []ipc.AttachEvent{
			{ID: 6, Kind: "task.claimed", TaskID: "t2", OccurredAt: at},
		},
	}
	var out, errOut bytes.Buffer
	if err := runEventsWith(context.Background(), &out, &errOut, f, "proj", 2, false); err != nil {
		t.Fatalf("runEventsWith: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (2 backlog + 1 live):\n%s", len(lines), out.String())
	}
	// Backlog is printed oldest-first, then the live event.
	if !strings.Contains(lines[0], "task.claimed") || !strings.Contains(lines[0], "task=t1") {
		t.Errorf("line 0 = %q, want the oldest backlog event (task.claimed t1)", lines[0])
	}
	if !strings.Contains(lines[1], "task.done") {
		t.Errorf("line 1 = %q, want task.done", lines[1])
	}
	if !strings.Contains(lines[2], "task.claimed") || !strings.Contains(lines[2], "task=t2") {
		t.Errorf("line 2 = %q, want the live event (task.claimed t2)", lines[2])
	}
	// The live cursor must be the backlog's max id — no gap, no duplicate.
	if f.gotAfterID != 5 {
		t.Errorf("live AfterID = %d, want 5 (the backlog max id)", f.gotAfterID)
	}
}

func TestRunEventsWith_ZeroBacklogTailsFromNow(t *testing.T) {
	f := &fakeEventSource{maxID: 42} // events exist, but --backlog 0 skips them
	var out, errOut bytes.Buffer
	if err := runEventsWith(context.Background(), &out, &errOut, f, "proj", 0, false); err != nil {
		t.Fatalf("runEventsWith: %v", err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Errorf("--backlog 0 printed backlog output: %q", out.String())
	}
	if f.gotAfterID != 42 {
		t.Errorf("live AfterID = %d, want 42 (max id, tail from now)", f.gotAfterID)
	}
}

func TestRunEventsWith_JSONEmitsValidJSONL(t *testing.T) {
	at := time.Date(2026, 7, 17, 8, 30, 0, 0, time.UTC)
	f := &fakeEventSource{
		maxID: 1,
		live: []ipc.AttachEvent{
			{ID: 2, Kind: "task.done", TaskID: "t1", OccurredAt: at},
		},
	}
	var out, errOut bytes.Buffer
	if err := runEventsWith(context.Background(), &out, &errOut, f, "proj", 0, true); err != nil {
		t.Fatalf("runEventsWith: %v", err)
	}
	line := strings.TrimSpace(out.String())
	var ev ipc.AttachEvent
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, line)
	}
	if ev.Kind != "task.done" || ev.ID != 2 {
		t.Errorf("decoded event = %+v, want kind task.done id 2", ev)
	}
}

func TestRunEventsWith_MidStreamErrorExitsNonZero(t *testing.T) {
	f := &fakeEventSource{maxID: 0, attachErr: errors.New("supervisor gone")}
	var out, errOut bytes.Buffer
	err := runEventsWith(context.Background(), &out, &errOut, f, "proj", 0, false)
	if err == nil {
		t.Fatal("want a non-nil error when the stream ends abnormally, got nil")
	}
	if !strings.Contains(errOut.String(), "event stream ended") {
		t.Errorf("stderr = %q, want an 'event stream ended' notice", errOut.String())
	}
}

func TestRunEventsWith_CtxCancelExitsClean(t *testing.T) {
	f := &fakeEventSource{maxID: 0, attachErr: context.Canceled}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // ctx already cancelled: a user interrupt
	var out, errOut bytes.Buffer
	if err := runEventsWith(ctx, &out, &errOut, f, "proj", 0, false); err != nil {
		t.Errorf("ctx-cancel (user interrupt) should exit clean, got %v", err)
	}
}

// TestLiveEventSourceBacklogCursorFromSameSnapshot proves the anti-race
// property: the live cursor is the newest id from the SAME supervisor snapshot
// that produced the printed rows, not a separate MaxEventID read.
func TestLiveEventSourceBacklogCursorFromSameSnapshot(t *testing.T) {
	client := &fakeObserveClient{snapshot: querySnapshotFixture(0)}
	client.snapshot.RecentEvents = observe.EventPage{
		Items: []observe.Event{
			{ID: 3, Kind: "third"},
			{ID: 2, Kind: "second"},
		},
		HasMore:      true,
		NextBeforeID: 2,
	}
	src := &liveEventSource{
		dial: func() (eventClient, error) { return client, nil },
	}

	events, cursor, err := src.Backlog(context.Background(), "project-1", 2)
	if err != nil {
		t.Fatalf("Backlog: %v", err)
	}
	if cursor != 3 {
		t.Errorf("cursor = %d, want newest snapshot event id 3", cursor)
	}
	if len(events) != 2 || events[0].ID != 2 || events[1].ID != 3 {
		t.Fatalf("events = %+v, want oldest-first ids [2 3]", events)
	}
	wantQuery := ipc.ObserveSnapshotArgs{
		ProjectID:  "project-1",
		PlanLimit:  1,
		TaskLimit:  1,
		EventLimit: 2,
	}
	if client.snapshotQ != wantQuery {
		t.Fatalf("snapshot query = %+v, want %+v", client.snapshotQ, wantQuery)
	}
}

func TestLiveEventSourceZeroBacklogSeedsFromSafeSnapshot(t *testing.T) {
	client := &fakeObserveClient{snapshot: querySnapshotFixture(0)}
	client.snapshot.RecentEvents = observe.EventPage{
		Items: []observe.Event{{ID: 42, Kind: "latest"}},
	}
	src := &liveEventSource{
		dial: func() (eventClient, error) { return client, nil },
	}

	events, cursor, err := src.Backlog(context.Background(), "project-1", 0)
	if err != nil {
		t.Fatalf("Backlog: %v", err)
	}
	if len(events) != 0 || cursor != 42 {
		t.Fatalf("zero backlog: events=%+v cursor=%d, want none and 42", events, cursor)
	}
	if client.snapshotQ.EventLimit != 1 {
		t.Fatalf("event limit = %d, want 1 for cursor seed", client.snapshotQ.EventLimit)
	}
}

func TestLiveEventSourceBacklogEmptyTailsFromZero(t *testing.T) {
	client := &fakeObserveClient{snapshot: querySnapshotFixture(0)}
	src := &liveEventSource{
		dial: func() (eventClient, error) { return client, nil },
	}

	events, cursor, err := src.Backlog(context.Background(), "project-1", 5)
	if err != nil {
		t.Fatalf("Backlog: %v", err)
	}
	if len(events) != 0 || cursor != 0 {
		t.Errorf("empty project: got %d events, cursor %d; want 0 events, cursor 0", len(events), cursor)
	}
}

func TestLiveEventSourceBacklogRejectsNegativeLimit(t *testing.T) {
	src := &liveEventSource{
		dial: func() (eventClient, error) {
			t.Fatal("negative backlog must fail before dialing")
			return nil, nil
		},
	}
	if _, _, err := src.Backlog(context.Background(), "project-1", -1); err == nil {
		t.Fatal("Backlog(-1) error = nil")
	}
}

func TestObserveEventToAttachDropsContentBearingFields(t *testing.T) {
	failure := &observe.FailureSummary{Category: observe.FailureDispatch}
	got := observeEventToAttach(observe.Event{
		ID:         7,
		Kind:       "worker.failed",
		Stream:     "task",
		PlanID:     "plan-1",
		TaskID:     "task-1",
		OccurredAt: time.Date(2026, time.July, 26, 20, 0, 0, 0, time.UTC),
		Failure:    failure,
	})
	if got.ID != 7 || got.Failure != failure {
		t.Fatalf("converted event = %+v", got)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal converted event: %v", err)
	}
	if strings.Contains(string(encoded), `"actor"`) ||
		strings.Contains(string(encoded), `"payload"`) {
		t.Fatalf("converted event retained legacy content: %s", encoded)
	}
}

func TestEventsCmd_Registered(t *testing.T) {
	root := newTestRootCmd(context.Background())
	var found *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "events" {
			found = c
			break
		}
	}
	if found == nil {
		t.Fatal("events command not registered on the root command")
	}
	// The flags the spec promises must exist.
	if found.Flags().Lookup("backlog") == nil {
		t.Error("events command missing --backlog flag")
	}
	if found.Flags().Lookup("json") == nil {
		t.Error("events command missing --json flag")
	}
}
