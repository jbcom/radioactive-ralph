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
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/spf13/cobra"
)

// fakeEventSource scripts a backlog + a live stream for runEventsWith.
type fakeEventSource struct {
	backlog    []store.Event
	maxID      int64
	live       []ipc.AttachEvent
	attachErr  error
	gotAfterID int64 // records the cursor the live attach was called with
}

func (f *fakeEventSource) Backlog(_ context.Context, _ string, n int) ([]store.Event, int64, error) {
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
		backlog: []store.Event{
			{ID: 4, Kind: "task.claimed", TaskID: "t1", Actor: "worker-1", OccurredAt: at},
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

// TestLiveEventSourceBacklogCursorFromSameRead proves the anti-race property:
// with a backlog, the live cursor is the newest id from the SAME query that
// produced the printed rows — not a separate MaxEventID read that could race a
// concurrent supervisor insert and duplicate an event across the boundary.
func TestLiveEventSourceBacklogCursorFromSameRead(t *testing.T) {
	ctx := context.Background()
	stateRoot := t.TempDir()
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	projectID, err := st.CreateProject(ctx, "events-backlog", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := st.Emit(ctx, store.EmitOpts{ProjectID: projectID, Kind: "tick"}); err != nil {
			t.Fatalf("Emit: %v", err)
		}
	}
	// The newest event's id is the cursor we expect.
	newest, err := st.ListProjectEvents(ctx, projectID, 1)
	if err != nil {
		t.Fatalf("ListProjectEvents: %v", err)
	}
	wantCursor := newest[0].ID
	_ = st.Close()

	src := &liveEventSource{stateRoot: stateRoot}
	events, cursor, err := src.Backlog(ctx, projectID, 2)
	if err != nil {
		t.Fatalf("Backlog: %v", err)
	}
	if cursor != wantCursor {
		t.Errorf("cursor = %d, want %d (the newest id, taken from the backlog read itself)", cursor, wantCursor)
	}
	// Backlog returns the 2 most recent, oldest-first; the LAST printed row's id
	// must equal the cursor (no printed row exceeds it → no live duplicate).
	if len(events) != 2 {
		t.Fatalf("got %d backlog events, want 2", len(events))
	}
	if events[len(events)-1].ID != cursor {
		t.Errorf("last backlog row id = %d, want it to equal the cursor %d", events[len(events)-1].ID, cursor)
	}
}

// TestLiveEventSourceBacklogEmptyTailsFromZero: an empty project with a backlog
// request tails from 0 (nothing to duplicate).
func TestLiveEventSourceBacklogEmptyTailsFromZero(t *testing.T) {
	ctx := context.Background()
	stateRoot := t.TempDir()
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	projectID, err := st.CreateProject(ctx, "events-empty", []store.Fingerprint{
		{Kind: store.FingerprintKindAbsPath, Value: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_ = st.Close()

	src := &liveEventSource{stateRoot: stateRoot}
	events, cursor, err := src.Backlog(ctx, projectID, 5)
	if err != nil {
		t.Fatalf("Backlog: %v", err)
	}
	if len(events) != 0 || cursor != 0 {
		t.Errorf("empty project: got %d events, cursor %d; want 0 events, cursor 0", len(events), cursor)
	}
}

func TestEventsCmd_Registered(t *testing.T) {
	root := newRootCmd(context.Background())
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
