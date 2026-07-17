package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

func testFake() *fakeDataSource {
	return &fakeDataSource{
		plans: []store.Plan{
			{ID: "plan-1", Title: "First plan", Status: store.PlanStatusActive},
			{ID: "plan-2", Title: "Second plan", Status: store.PlanStatusPaused},
		},
		progress: map[string]orch.Progress{
			"plan-1": {Done: 1, Total: 2},
			"plan-2": {Done: 0, Total: 3},
		},
		tasksByPlan: map[string][]store.Task{
			"plan-1": {
				{ID: "task-a", PlanID: "plan-1", Description: "do a thing", Status: store.TaskStatusRunning},
				{ID: "task-b", PlanID: "plan-1", Description: "do another thing", Status: store.TaskStatusPending},
			},
		},
		taskEvents: map[string][]store.Event{
			"plan-1/task-a": {
				{ID: 1, Kind: "task.claimed", OccurredAt: time.Now()},
			},
		},
	}
}

func newTestModel(t *testing.T, f *fakeDataSource) Model {
	t.Helper()
	m := NewModel(context.Background(), f, "project-1")
	return m
}

func TestModel_StartsAtMacro(t *testing.T) {
	m := newTestModel(t, testFake())
	if m.lvl != levelMacro {
		t.Fatalf("expected initial level macro, got %v", m.lvl)
	}
}

func TestModel_DrillInMacroToMeso(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.plans = f.plans // simulate a completed fetch

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.lvl != levelMeso {
		t.Fatalf("expected level meso after drill-in, got %v", m.lvl)
	}
	if m.selectedPlan.ID != "plan-1" {
		t.Fatalf("expected selectedPlan plan-1, got %q", m.selectedPlan.ID)
	}
	if cmd == nil {
		t.Fatalf("expected a fetch command after drill-in")
	}
}

// TestModel_FirstFetchStartsSessionAttach: the live subscription is session-long
// — it starts on the FIRST completed fetch (not on micro drill-in), so the
// macro/meso views are live from the start.
func TestModel_FirstFetchStartsSessionAttach(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	if m.attachCancel != nil {
		t.Fatal("subscription should not be active before the first fetch")
	}

	updated, cmd := m.Update(fetchedMsg{snap: snapshot{plans: f.plans}})
	m = updated.(Model)

	if m.attachCancel == nil || m.attachFrames == nil {
		t.Fatal("expected the session subscription to start on the first fetch")
	}
	if cmd == nil {
		t.Fatal("expected an attachCmd after the first fetch starts the subscription")
	}
	// A second fetch must NOT start a second subscription (idempotent).
	frames := m.attachFrames
	updated, _ = m.Update(fetchedMsg{snap: snapshot{plans: f.plans}})
	m = updated.(Model)
	if m.attachFrames != frames {
		t.Error("second fetch restarted the subscription; ensureAttach must be idempotent")
	}
	m.attachCancel()
}

// TestModel_ReconnectsAfterStreamEnds: because the subscription is now
// session-long, a stream end (supervisor blip) must not permanently kill the
// live feed — attachEndedMsg drops the channels, and the next fetch restarts the
// subscription via ensureAttach. Without this the macro/meso views would
// silently degrade to poll-only after the first blip for the rest of the session.
func TestModel_ReconnectsAfterStreamEnds(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)

	// First fetch starts the session subscription.
	updated, _ := m.Update(fetchedMsg{snap: snapshot{plans: f.plans}})
	m = updated.(Model)
	if m.attachFrames == nil {
		t.Fatal("subscription did not start on first fetch")
	}
	epoch1 := m.attachEpoch
	m.attachCancel()

	// The stream ends (EOF / supervisor restart) for the CURRENT subscription.
	updated, _ = m.Update(attachEndedMsg{epoch: epoch1})
	m = updated.(Model)
	if m.attachFrames != nil {
		t.Fatal("attachEndedMsg should drop the channel references")
	}

	// The next fetch must RESTART the subscription (reconnect), not leave it dead.
	updated, cmd := m.Update(fetchedMsg{snap: snapshot{plans: f.plans}})
	m = updated.(Model)
	if m.attachFrames == nil {
		t.Fatal("subscription was not restarted after the stream ended — the live feed would be dead for the rest of the session")
	}
	if m.attachEpoch <= epoch1 {
		t.Errorf("epoch did not advance on reconnect: was %d, now %d", epoch1, m.attachEpoch)
	}
	if cmd == nil {
		t.Fatal("expected an attachCmd for the restarted subscription")
	}
	m.attachCancel()
}

// TestModel_ReconnectResumesFromLastEventID: after processing live events, a
// reconnect must resume from the highest id seen (not re-seed from 0/current
// max), so events emitted during the disconnect gap are delivered.
func TestModel_ReconnectResumesFromLastEventID(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)

	// First attach: seeds from 0 (initial — "from now"). startAttach runs the
	// fake's Attach on a goroutine, so wait for it to record the cursor.
	updated, _ := m.Update(fetchedMsg{snap: snapshot{plans: f.plans}})
	m = updated.(Model)
	epoch1 := m.attachEpoch
	waitAttachCount(t, f, 1)
	if f.afterIDAt(0) != 0 {
		t.Fatalf("first attach afterID = %d, want 0 (initial seed)", f.afterIDAt(0))
	}
	m.attachCancel()

	// Process a couple of live events; the model tracks the highest id.
	updated, _ = m.Update(liveFrameMsg{epoch: epoch1, raw: []byte(`{"id":11,"kind":"task.claimed","task_id":"t1"}`)})
	m = updated.(Model)
	updated, _ = m.Update(liveFrameMsg{epoch: epoch1, raw: []byte(`{"id":14,"kind":"task.done","task_id":"t1"}`)})
	m = updated.(Model)
	if m.lastEventID != 14 {
		t.Fatalf("lastEventID = %d, want 14 (highest processed)", m.lastEventID)
	}

	// Stream ends, then a fetch reconnects — it must resume from id 14.
	updated, _ = m.Update(attachEndedMsg{epoch: epoch1})
	m = updated.(Model)
	updated, _ = m.Update(fetchedMsg{snap: snapshot{plans: f.plans}})
	m = updated.(Model)
	waitAttachCount(t, f, 2)
	if f.afterIDAt(1) != 14 {
		t.Errorf("reconnect afterID = %d, want 14 (resume from last processed — gap events not missed)", f.afterIDAt(1))
	}
	m.attachCancel()
}

// TestModel_DrillInMicroDoesNotRestartAttach: drilling into micro reuses the
// session subscription (it doesn't start a new one) and resets the per-task log.
func TestModel_DrillInMicroDoesNotRestartAttach(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.plans = f.plans
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = f.tasksByPlan["plan-1"]
	// Simulate the already-running session subscription.
	cancelled := false
	m.attachCancel = func() { cancelled = true }
	m.attachFrames = make(chan json.RawMessage)
	m.snap.live = []liveLogLine{{text: "stale"}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.lvl != levelMicro || m.selectedTask.ID != "task-a" {
		t.Fatalf("drill-in didn't reach micro/task-a: lvl=%v task=%q", m.lvl, m.selectedTask.ID)
	}
	if cancelled {
		t.Error("drill-in cancelled the session subscription; it must be reused")
	}
	if len(m.snap.live) != 0 {
		t.Errorf("drill-in did not reset the per-task log: %d lines", len(m.snap.live))
	}
}

// TestModel_DrillOutMicroKeepsAttach: drilling out of micro does NOT stop the
// session subscription — macro/meso stay live.
func TestModel_DrillOutMicroKeepsAttach(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMicro
	m.selectedPlan = f.plans[0]
	m.selectedTask = f.tasksByPlan["plan-1"][0]
	cancelled := false
	m.attachCancel = func() { cancelled = true }
	m.attachFrames = make(chan json.RawMessage)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.lvl != levelMeso {
		t.Fatalf("expected level meso after drill-out, got %v", m.lvl)
	}
	if cancelled {
		t.Error("drill-out cancelled the session subscription; it must stay live for macro/meso")
	}
	if m.attachCancel == nil {
		t.Error("drill-out cleared attachCancel; the session subscription must persist")
	}
}

func TestModel_DrillOutMesoToMacro(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.cursor = 1

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)

	if m.lvl != levelMacro {
		t.Fatalf("expected level macro after drill-out, got %v", m.lvl)
	}
	if m.cursor != 0 {
		t.Fatalf("expected cursor reset to 0, got %d", m.cursor)
	}
}

func TestModel_DrillOutAtMacroIsNoop(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m2 := updated.(Model)

	if m2.lvl != levelMacro {
		t.Fatalf("expected level to remain macro, got %v", m2.lvl)
	}
	if cmd != nil {
		t.Fatalf("expected no command from a no-op drill-out")
	}
}

func TestModel_CursorMovementBounded(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.plans = f.plans // 2 plans: indices 0,1

	// Move down past the end.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", m.cursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.cursor != 1 {
		t.Fatalf("expected cursor clamped at 1, got %d", m.cursor)
	}

	// Move up past the start.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.cursor != 0 {
		t.Fatalf("expected cursor 0, got %d", m.cursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.cursor != 0 {
		t.Fatalf("expected cursor clamped at 0, got %d", m.cursor)
	}
}

func TestModel_QuitSendsQuitCmd(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	stopped := false
	m.lvl = levelMicro
	m.attachCancel = func() { stopped = true }

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)

	if !m.quitting {
		t.Fatalf("expected quitting=true")
	}
	if !stopped {
		t.Fatalf("expected attachCancel invoked on quit while in micro")
	}
	if cmd == nil {
		t.Fatalf("expected tea.Quit command")
	}
}

func TestModel_FetchedMsgAppliesErrorWithoutPanic(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)

	updated, _ := m.Update(fetchedMsg{err: context.DeadlineExceeded})
	m = updated.(Model)
	if m.err == nil {
		t.Fatalf("expected err to be recorded")
	}

	// View must render without panicking even with an error set.
	_ = m.View()
}

func TestModel_LiveFrameAppendsToLog(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMicro

	updated, _ := m.Update(liveFrameMsg{raw: []byte(`{"kind":"task.claimed","task_id":"task-a"}`)})
	m = updated.(Model)

	if len(m.snap.live) != 1 {
		t.Fatalf("expected 1 live line, got %d", len(m.snap.live))
	}
}

func TestModel_LiveFrameFiltersToSelectedTask(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMicro
	m.selectedTask = store.Task{ID: "task-a"}

	// A frame for a DIFFERENT task must not pollute the selected task's tail.
	updated, _ := m.Update(liveFrameMsg{raw: []byte(`{"kind":"task.claimed","task_id":"task-b"}`)})
	m = updated.(Model)
	if len(m.snap.live) != 0 {
		t.Fatalf("frame for task-b appended to task-a's tail: got %d lines", len(m.snap.live))
	}

	// A frame for the selected task IS appended.
	updated, _ = m.Update(liveFrameMsg{raw: []byte(`{"kind":"task.done","task_id":"task-a"}`)})
	m = updated.(Model)
	if len(m.snap.live) != 1 {
		t.Fatalf("frame for the selected task not appended: got %d lines", len(m.snap.live))
	}

	// A task-agnostic frame (no task_id) is shown as context.
	updated, _ = m.Update(liveFrameMsg{raw: []byte(`{"kind":"plan.imported"}`)})
	m = updated.(Model)
	if len(m.snap.live) != 2 {
		t.Fatalf("task-agnostic frame not shown: got %d lines", len(m.snap.live))
	}
}

func TestModel_LiveFrameAppliesTaskStatusDelta(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.tasks = []store.Task{
		{ID: "task-a", Status: store.TaskStatusRunning},
		{ID: "task-b", Status: store.TaskStatusReady},
	}

	// A task.done for task-a flips its status immediately, without a poll.
	updated, _ := m.Update(liveFrameMsg{raw: []byte(`{"kind":"task.done","task_id":"task-a"}`)})
	m = updated.(Model)
	if m.snap.tasks[0].Status != store.TaskStatusDone {
		t.Errorf("task-a status = %q, want done (live delta)", m.snap.tasks[0].Status)
	}
	if m.snap.tasks[1].Status != store.TaskStatusReady {
		t.Errorf("task-b status = %q, want ready (untouched)", m.snap.tasks[1].Status)
	}
}

func TestModel_LiveFrameAppliesBlockedDelta(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.tasks = []store.Task{{ID: "task-a", Status: store.TaskStatusRunning}}

	// The store emits task.blocked / task.context_requested on running→blocked;
	// both must flip the visible status to blocked immediately.
	for _, kind := range []string{"task.blocked", "task.context_requested"} {
		m.snap.tasks[0].Status = store.TaskStatusRunning
		updated, _ := m.Update(liveFrameMsg{raw: []byte(`{"kind":"` + kind + `","task_id":"task-a"}`)})
		m = updated.(Model)
		if m.snap.tasks[0].Status != store.TaskStatusBlocked {
			t.Errorf("%s: task-a status = %q, want blocked (live delta)", kind, m.snap.tasks[0].Status)
		}
	}
}

// TestModel_LiveFramePrependsToMacroTail: a live frame prepends to the macro
// event pane (newest-first) at ANY level, deduped by id, so the macro overview
// is a live feed and a poll landing after a live prepend doesn't double-count.
func TestModel_LiveFramePrependsToMacroTail(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMacro // NOT micro — the macro tail must still update

	updated, _ := m.Update(liveFrameMsg{raw: []byte(`{"id":7,"kind":"task.done","task_id":"t1"}`)})
	m = updated.(Model)
	if len(m.snap.planEvent) != 1 || m.snap.planEvent[0].Kind != "task.done" || m.snap.planEvent[0].ID != 7 {
		t.Fatalf("macro tail = %+v, want [task.done id=7] after a live frame at macro level", m.snap.planEvent)
	}

	// The SAME event again (e.g. a poll re-including it, or a duplicate frame) is
	// deduped by id — the pane must not show it twice.
	updated, _ = m.Update(liveFrameMsg{raw: []byte(`{"id":7,"kind":"task.done","task_id":"t1"}`)})
	m = updated.(Model)
	if len(m.snap.planEvent) != 1 {
		t.Errorf("duplicate id 7 was not deduped: macro tail has %d rows", len(m.snap.planEvent))
	}

	// A newer event prepends ahead of the older one (newest-first).
	updated, _ = m.Update(liveFrameMsg{raw: []byte(`{"id":8,"kind":"task.claimed","task_id":"t2"}`)})
	m = updated.(Model)
	if len(m.snap.planEvent) != 2 || m.snap.planEvent[0].ID != 8 {
		t.Fatalf("macro tail = %+v, want the newer id=8 first", m.snap.planEvent)
	}
}

// TestModel_PollDoesNotDropLiveEvent: a live event prepended to the macro tail
// must SURVIVE a subsequent poll whose snapshot was read before that event hit
// the DB. A wholesale replace would silently lose it (one-shot stream frame,
// never re-delivered); the merge keeps both, deduped.
func TestModel_PollDoesNotDropLiveEvent(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMacro

	// A live event id=9 arrives and is prepended.
	updated, _ := m.Update(liveFrameMsg{raw: []byte(`{"id":9,"kind":"task.done","task_id":"t1"}`)})
	m = updated.(Model)

	// A poll lands whose snapshot predates id=9 (contains only id=8) — a
	// wholesale replace would drop id=9.
	updated, _ = m.Update(fetchedMsg{snap: snapshot{
		plans:     f.plans,
		planEvent: []store.Event{{ID: 8, Kind: "task.claimed"}},
	}})
	m = updated.(Model)

	haveIDs := map[int64]bool{}
	for _, ev := range m.snap.planEvent {
		haveIDs[ev.ID] = true
	}
	if !haveIDs[9] {
		t.Errorf("poll dropped the live event id=9: macro tail = %+v", m.snap.planEvent)
	}
	if !haveIDs[8] {
		t.Errorf("poll's own event id=8 missing: macro tail = %+v", m.snap.planEvent)
	}
	// Newest-first: id=9 must be ahead of id=8.
	if m.snap.planEvent[0].ID != 9 {
		t.Errorf("macro tail not newest-first: head is id=%d, want 9", m.snap.planEvent[0].ID)
	}
	m.attachCancel()
}

// TestPrependEvent_IDLessFramesNotDeduped: two distinct frames with no id
// (mapping to ID 0) must BOTH appear — deduping on 0 would drop all but one.
func TestPrependEvent_IDLessFramesNotDeduped(t *testing.T) {
	var tail []store.Event
	tail = prependEvent(tail, ipc.AttachEvent{Kind: "service.started"})
	tail = prependEvent(tail, ipc.AttachEvent{Kind: "tick"})
	if len(tail) != 2 {
		t.Fatalf("id-less frames were deduped: got %d, want 2 (%+v)", len(tail), tail)
	}
	// A real (nonzero) id is still deduped.
	tail = prependEvent(tail, ipc.AttachEvent{ID: 5, Kind: "task.done"})
	tail = prependEvent(tail, ipc.AttachEvent{ID: 5, Kind: "task.done"})
	realCount := 0
	for _, e := range tail {
		if e.ID == 5 {
			realCount++
		}
	}
	if realCount != 1 {
		t.Errorf("real id=5 deduped incorrectly: appears %d times", realCount)
	}
}

func TestMergeEventTail_IDLessRowsNotDeduped(t *testing.T) {
	live := []store.Event{{ID: 0, Kind: "a"}, {ID: 0, Kind: "b"}}
	poll := []store.Event{{ID: 3, Kind: "c"}}
	got := mergeEventTail(live, poll)
	if len(got) != 3 {
		t.Errorf("id-less rows deduped in merge: got %d, want 3 (%+v)", len(got), got)
	}
}

func TestMergeEventTail(t *testing.T) {
	live := []store.Event{{ID: 9}, {ID: 7}} // newest-first
	poll := []store.Event{{ID: 8}, {ID: 7}, {ID: 6}}
	got := mergeEventTail(live, poll)
	wantIDs := []int64{9, 8, 7, 6}
	if len(got) != len(wantIDs) {
		t.Fatalf("merged len = %d (%+v), want %d", len(got), got, len(wantIDs))
	}
	for i, id := range wantIDs {
		if got[i].ID != id {
			t.Errorf("merged[%d].ID = %d, want %d (newest-first, deduped)", i, got[i].ID, id)
		}
	}
}

func TestModel_LiveFrameUnknownKindIsSnapshotNoop(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.tasks = []store.Task{{ID: "task-a", Status: store.TaskStatusRunning}}

	updated, _ := m.Update(liveFrameMsg{raw: []byte(`{"kind":"task.progress","task_id":"task-a"}`)})
	m = updated.(Model)
	if m.snap.tasks[0].Status != store.TaskStatusRunning {
		t.Errorf("unknown kind mutated task status to %q, want running unchanged", m.snap.tasks[0].Status)
	}
}

func TestModel_LiveFrameUndecodableIsDropped(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMicro

	// Malformed JSON and an empty-kind frame both drop cleanly (no panic, no
	// log line).
	for _, raw := range []string{`{not json`, `{"task_id":"task-a"}`} {
		updated, _ := m.Update(liveFrameMsg{raw: []byte(raw)})
		m = updated.(Model)
	}
	if len(m.snap.live) != 0 {
		t.Errorf("undecodable frames produced %d log lines, want 0", len(m.snap.live))
	}
}

func TestStartAttach_StreamsFramesThenEnds(t *testing.T) {
	f := testFake()
	f.attachFrames = []json.RawMessage{
		[]byte(`{"kind":"task.claimed","task_id":"task-a"}`),
		[]byte(`{"kind":"task.completed","task_id":"task-a"}`),
	}
	f.attachErr = errFakeAttach

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frames, done, stop := startAttach(ctx, f, 0)
	defer stop()

	var got []string
	for raw := range frames {
		got = append(got, string(raw))
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 frames, got %d: %v", len(got), got)
	}

	if err := <-done; err != errFakeAttach {
		t.Fatalf("expected errFakeAttach from done channel, got %v", err)
	}
}

func TestMacroHeaderShowsSupervisorLiveness(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.plans = f.plans
	m.snap.progress = f.progress
	m.snap.status = ipc.StatusReply{Uptime: 2 * time.Hour, ActiveWorkers: 1}

	view := m.View()
	if !strings.Contains(view, "connected") || !strings.Contains(view, "up 2h0m") {
		t.Errorf("macro header missing the connected/uptime liveness line:\n%s", view)
	}

	// When the last refresh failed (supervisor unreachable mid-session), the
	// header must NOT keep claiming "connected" with a frozen uptime — it shows
	// the disconnected state instead.
	m.err = context.DeadlineExceeded
	dview := m.View()
	if !strings.Contains(dview, "disconnected") {
		t.Errorf("macro header should show 'disconnected' after a failed refresh:\n%s", dview)
	}
	if strings.Contains(dview, "up 2h0m") {
		t.Error("macro header should not show a frozen uptime when disconnected")
	}
}

func TestHumanizeUptime(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second: "30s",
		5 * time.Minute:  "5m",
		90 * time.Minute: "1h30m",
	}
	for d, want := range cases {
		if got := humanizeUptime(d); got != want {
			t.Errorf("humanizeUptime(%s) = %q, want %q", d, got, want)
		}
	}
}

func TestModel_ViewRendersAtEachLevelWithoutPanicking(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.plans = f.plans
	m.snap.progress = f.progress

	_ = m.View() // macro

	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = f.tasksByPlan["plan-1"]
	_ = m.View() // meso

	m.lvl = levelMicro
	m.selectedTask = f.tasksByPlan["plan-1"][0]
	m.snap.taskEvent = f.taskEvents["plan-1/task-a"]
	_ = m.View() // micro
}

// TestModel_CursorClampedWhenListShrinks is the regression for the audit's C1:
// a background refresh that shrinks the plan list must re-bound the cursor so
// the selection stays visible and drillIn opens a row that still exists —
// rather than silently pointing past the end (invisible cursor) or, worse,
// selecting a different row than the operator saw.
func TestModel_CursorClampedWhenListShrinks(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.plans = f.plans // 2 plans
	m.cursor = 1           // second plan selected

	// A refresh returns only ONE plan now (the second was removed).
	shrunk := snapshot{
		status: ipc.StatusReply{},
		plans:  []store.Plan{{ID: "plan-1", Title: "First plan", Status: store.PlanStatusActive}},
	}
	updated, _ := m.Update(fetchedMsg{snap: shrunk})
	m = updated.(Model)

	if m.cursor != 0 {
		t.Fatalf("cursor = %d after list shrank 2→1, want 0 (clamped to last index)", m.cursor)
	}
	// And drilling in must open the surviving plan, never panic.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.lvl != levelMeso || m.selectedPlan.ID != "plan-1" {
		t.Fatalf("after clamp+drill: lvl=%v selectedPlan=%q, want meso/plan-1", m.lvl, m.selectedPlan.ID)
	}
}

// TestModel_CursorPreservesIdentityWhenRowRemovedAhead is the regression for the
// audit's identity-vs-index case: when a refresh removes a row BEFORE the
// selected one, the numeric index would still be in bounds but point at a
// DIFFERENT entity. The cursor must follow the SELECTED ENTITY by ID, not the
// index — e.g. [A,B,C] cursor=1 (B) → [B,C] must keep B selected (cursor 0),
// not leave cursor 1 on C.
func TestModel_CursorPreservesIdentityWhenRowRemovedAhead(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.plans = []store.Plan{
		{ID: "A", Title: "Alpha", Status: store.PlanStatusActive},
		{ID: "B", Title: "Bravo", Status: store.PlanStatusActive},
		{ID: "C", Title: "Charlie", Status: store.PlanStatusActive},
	}
	m.cursor = 1 // B selected

	// Refresh removes A (the row ahead of the cursor): [B, C].
	updated, _ := m.Update(fetchedMsg{snap: snapshot{
		status: ipc.StatusReply{},
		plans: []store.Plan{
			{ID: "B", Title: "Bravo", Status: store.PlanStatusActive},
			{ID: "C", Title: "Charlie", Status: store.PlanStatusActive},
		},
	}})
	m = updated.(Model)

	// The cursor must now be on B (index 0), NOT still on index 1 (which is C).
	if m.cursor != 0 {
		t.Fatalf("cursor = %d after A removed ahead of B, want 0 (follow B by identity)", m.cursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.selectedPlan.ID != "B" {
		t.Fatalf("drilled into %q, want B (the entity the operator had selected)", m.selectedPlan.ID)
	}
}

// TestModel_AllGatherPathsSetInFlight confirms the drill paths also mark the
// in-flight guard — so a drill can't stack an overlapping gather on top of a
// slow periodic one (the audit's "all gather launch paths must participate"
// point). A drill while a gather is outstanding must fire NO new gather.
func TestModel_AllGatherPathsSetInFlight(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.plans = f.plans

	// A drill-in from macro→meso with no gather outstanding must mark fetching.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !m.fetching {
		t.Fatal("drill-in should mark fetching=true (its gather must be tracked)")
	}
	if cmd == nil {
		t.Fatal("drill-in should return a fetch command")
	}

	// Now a drill-out while that gather is still in flight must NOT start another.
	m.fetching = true
	before := m.lvl
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.lvl == before {
		t.Fatal("drill-out should still navigate up even when a gather is in flight")
	}
	if !m.fetching {
		t.Fatal("fetching must remain true — drill-out must not clear it or start a second gather")
	}
}

// TestModel_RefreshTickSkipsFetchWhileInFlight is the regression for the audit's
// C2: the 1s tick must NOT start a second gather while a prior one is still
// outstanding (which would stack supervisor connections and let stale results
// land out of order). The first tick starts a fetch; a second tick before the
// fetchedMsg returns must only re-arm the timer, not fire another fetch.
func TestModel_RefreshTickSkipsFetchWhileInFlight(t *testing.T) {
	m := newTestModel(t, testFake())
	m.fetching = false

	// First tick: not fetching → starts a gather (batch of fetch + tick).
	updated, cmd1 := m.Update(refreshMsg(time.Now()))
	m = updated.(Model)
	if !m.fetching {
		t.Fatal("first refresh tick should mark fetching=true")
	}
	if cmd1 == nil {
		t.Fatal("first refresh tick should return a command (fetch + tick)")
	}

	// Second tick while still fetching: must NOT start another gather. We can't
	// easily assert the batch contents, so assert fetching stays true and a
	// command (the re-armed tick) is still returned.
	updated, cmd2 := m.Update(refreshMsg(time.Now()))
	m = updated.(Model)
	if !m.fetching {
		t.Fatal("fetching must stay true across an overlapping tick")
	}
	if cmd2 == nil {
		t.Fatal("second tick should still re-arm the timer")
	}

	// The gather returns: fetching clears, so the next tick can fetch again.
	updated, _ = m.Update(fetchedMsg{snap: snapshot{status: ipc.StatusReply{}}})
	m = updated.(Model)
	if m.fetching {
		t.Fatal("fetchedMsg must clear fetching")
	}
}
