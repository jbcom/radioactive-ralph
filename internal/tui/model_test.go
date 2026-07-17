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

func TestModel_DrillInMesoToMicroStartsAttach(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.snap.plans = f.plans
	m.lvl = levelMeso
	m.selectedPlan = f.plans[0]
	m.snap.tasks = f.tasksByPlan["plan-1"]

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.lvl != levelMicro {
		t.Fatalf("expected level micro after drill-in, got %v", m.lvl)
	}
	if m.selectedTask.ID != "task-a" {
		t.Fatalf("expected selectedTask task-a, got %q", m.selectedTask.ID)
	}
	if m.attachCancel == nil {
		t.Fatalf("expected attachCancel to be set once micro is entered")
	}
	if cmd == nil {
		t.Fatalf("expected a batched fetch+attach command")
	}
	// Clean up the goroutine started by drilling in.
	m.attachCancel()
}

func TestModel_DrillOutMicroToMesoStopsAttach(t *testing.T) {
	f := testFake()
	m := newTestModel(t, f)
	m.lvl = levelMicro
	m.selectedPlan = f.plans[0]
	m.selectedTask = f.tasksByPlan["plan-1"][0]
	stopped := false
	m.attachCancel = func() { stopped = true }

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.lvl != levelMeso {
		t.Fatalf("expected level meso after drill-out, got %v", m.lvl)
	}
	if !stopped {
		t.Fatalf("expected attachCancel to be invoked on drill-out")
	}
	if m.attachCancel != nil {
		t.Fatalf("expected attachCancel to be cleared after drill-out")
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

func TestStartAttach_StreamsFramesThenEnds(t *testing.T) {
	f := testFake()
	f.attachFrames = []json.RawMessage{
		[]byte(`{"kind":"task.claimed","task_id":"task-a"}`),
		[]byte(`{"kind":"task.completed","task_id":"task-a"}`),
	}
	f.attachErr = errFakeAttach

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frames, done, stop := startAttach(ctx, f)
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
