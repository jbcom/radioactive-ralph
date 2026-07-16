package tui

import (
	"bytes"
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestE2E_DrillNavigation is the Layer-1 E2E harness (Phase 8): it drives
// the real tea.Model — not Update called directly, as model_test.go does —
// through teatest.NewTestModel, sending real tea.KeyMsg keystrokes exactly
// as a user's terminal would produce them, and asserting on the RENDERED
// terminal output at each drill level. This is model-level, CI-feasible,
// and deterministic: no real supervisor, no pty, just a FakeDataSource
// seeded with plans/tasks/events and the actual Bubble Tea render loop.
//
// Per the E2E strategy (.agent-state/decisions.ndjson,
// "tui-e2e-testing-strategy" + directive-additions.md "E2E testing
// tooling"), this intentionally asserts on SUBSTRINGS of the rendered
// output via teatest.WaitFor rather than teatest.RequireEqualOutput/golden
// files: golden output is color-profile- and line-ending-sensitive across
// CI runners, so a substring check is the reliable signal that drill
// navigation actually rendered the right content, without being flaky on
// terminal-rendering minutiae this test doesn't care about.
func TestE2E_DrillNavigation(t *testing.T) {
	fake := &fakeDataSource{
		status: ipc.StatusReply{
			ActiveWorkers: 2,
			ReadyTasks:    1,
			RunningTasks:  1,
		},
		plans: []store.Plan{
			{ID: "plan-1", Title: "Ship the widget", Status: store.PlanStatusActive},
			{ID: "plan-2", Title: "Retire the gadget", Status: store.PlanStatusPaused},
		},
		progress: map[string]orch.Progress{
			"plan-1": {Done: 1, Total: 3},
			"plan-2": {Done: 0, Total: 2},
		},
		tasksByPlan: map[string][]store.Task{
			"plan-1": {
				{ID: "task-a", PlanID: "plan-1", Description: "wire up the frobnicator", Status: store.TaskStatusRunning},
				{ID: "task-b", PlanID: "plan-1", Description: "polish the gizmo", Status: store.TaskStatusPending},
			},
		},
		taskEvents: map[string][]store.Event{
			"plan-1/task-a": {
				{ID: 1, Kind: "task.claimed", OccurredAt: time.Now()},
				{ID: 2, Kind: "task.progress", OccurredAt: time.Now()},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModel(ctx, fake, "project-1")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	// --- macro: the plan list must render both seeded plans. ---
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Ship the widget")) &&
			bytes.Contains(b, []byte("Retire the gadget"))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Drill in: macro -> meso (plan-1 is the cursor's first row).
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// --- meso: the selected plan's title and its tasks must render. ---
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Ship the widget")) &&
			bytes.Contains(b, []byte("wire up the frobnicator")) &&
			bytes.Contains(b, []byte("polish the gizmo"))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Drill in: meso -> micro (task-a is the cursor's first row).
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// --- micro: the selected task's id/description and its event tail. ---
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("task-a")) &&
			bytes.Contains(b, []byte("wire up the frobnicator")) &&
			bytes.Contains(b, []byte("task.claimed"))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Drill out: micro -> meso. The meso view (task list) must reappear.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("wire up the frobnicator")) &&
			bytes.Contains(b, []byte("back to plans"))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Drill out: meso -> macro. Both plans must reappear at the top level.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Ship the widget")) &&
			bytes.Contains(b, []byte("Retire the gadget")) &&
			bytes.Contains(b, []byte("enter: drill into plan"))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Quit cleanly.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

// TestE2E_DownArrowSelectsSecondPlan confirms the down-arrow keystroke
// actually moves the macro-level cursor before drilling in — i.e. drill
// navigation honors the operator's selection, not just "always drill into
// row 0". This is the other half of "prove the drill navigation works":
// Layer 1 must show that arrow keys change what gets drilled into.
func TestE2E_DownArrowSelectsSecondPlan(t *testing.T) {
	fake := &fakeDataSource{
		plans: []store.Plan{
			{ID: "plan-1", Title: "Ship the widget", Status: store.PlanStatusActive},
			{ID: "plan-2", Title: "Retire the gadget", Status: store.PlanStatusPaused},
		},
		progress: map[string]orch.Progress{
			"plan-1": {Done: 1, Total: 3},
			"plan-2": {Done: 0, Total: 2},
		},
		tasksByPlan: map[string][]store.Task{
			"plan-2": {
				{ID: "task-z", PlanID: "plan-2", Description: "decommission the gadget", Status: store.TaskStatusPending},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewModel(ctx, fake, "project-1")
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Ship the widget")) && bytes.Contains(b, []byte("Retire the gadget"))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	// Move the cursor down to plan-2, then drill in.
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Retire the gadget")) &&
			bytes.Contains(b, []byte("decommission the gadget"))
	}, teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}
