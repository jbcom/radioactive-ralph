//go:build gui

package gui

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// newTestUI builds a ui backed by a fake controller and the headless test
// driver, with the given plans/tasks/events preloaded.
func newTestUI(t *testing.T, f *fakeController) *ui {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	w := a.NewWindow("test")
	u := newUI(context.Background(), f, "proj", w)
	u.syncRender = true // render + drive inline so taps are immediately assertable
	w.SetContent(u.root)
	return u
}

// findButton walks a canvas-object tree and returns the first *widget.Button
// whose label equals text, or nil.
func findButton(obj fyne.CanvasObject, text string) *widget.Button {
	switch o := obj.(type) {
	case *widget.Button:
		if o.Text == text {
			return o
		}
	case *fyne.Container:
		for _, c := range o.Objects {
			if b := findButton(c, text); b != nil {
				return b
			}
		}
	case *container.Scroll:
		return findButton(o.Content, text)
	}
	return nil
}

func tapButton(t *testing.T, root fyne.CanvasObject, text string) {
	t.Helper()
	b := findButton(root, text)
	if b == nil {
		t.Fatalf("button %q not found in view", text)
	}
	test.Tap(b)
}

// forEachLabel visits every *widget.Label in the tree.
func forEachLabel(obj fyne.CanvasObject, fn func(*widget.Label)) {
	switch o := obj.(type) {
	case *widget.Label:
		fn(o)
	case *fyne.Container:
		for _, c := range o.Objects {
			forEachLabel(c, fn)
		}
	case *container.Scroll:
		forEachLabel(o.Content, fn)
	}
}

// labelExists reports whether some label's text equals text exactly.
func labelExists(root fyne.CanvasObject, text string) bool {
	found := false
	forEachLabel(root, func(l *widget.Label) {
		if l.Text == text {
			found = true
		}
	})
	return found
}

// labelContains reports whether some label's text contains substr.
func labelContains(root fyne.CanvasObject, substr string) bool {
	found := false
	forEachLabel(root, func(l *widget.Label) {
		if strings.Contains(l.Text, substr) {
			found = true
		}
	})
	return found
}

func TestMacro_RendersPlansAndDrillsToMeso(t *testing.T) {
	f := newFakeController()
	f.plans = []store.Plan{{ID: "p1", Title: "Ship It", Status: store.PlanStatusActive}}
	f.tasks["p1"] = []store.Task{{ID: "t1", Description: "do the thing", Status: store.TaskStatusPending}}
	u := newTestUI(t, f)

	u.refreshNow()
	if findButton(u.root, "Ship It") == nil {
		t.Fatal("macro view did not render the plan as a button")
	}

	// Drill into the plan.
	tapButton(t, u.root, "Ship It")
	if u.selectedPlan != "p1" {
		t.Fatalf("selectedPlan = %q, want p1", u.selectedPlan)
	}
	// Meso should now show the task and the plan controls.
	if findButton(u.root, "Pause") == nil {
		t.Error("meso view missing Pause control")
	}
	if findButton(u.root, "do the thing") == nil {
		t.Error("meso view did not render the task")
	}
}

func TestMeso_PauseCallsSetPlanStatus(t *testing.T) {
	f := newFakeController()
	f.plans = []store.Plan{{ID: "p1", Title: "P", Status: store.PlanStatusActive}}
	u := newTestUI(t, f)
	u.selectedPlan = "p1"
	u.refreshNow()

	tapButton(t, u.root, "Pause")
	calls := f.setStatusCalls()
	if len(calls) != 1 || calls[0] != [2]string{"p1", "paused"} {
		t.Fatalf("SetPlanStatus calls = %v, want one {p1 paused}", calls)
	}

	tapButton(t, u.root, "Abandon")
	calls = f.setStatusCalls()
	if len(calls) != 2 || calls[1] != [2]string{"p1", "abandoned"} {
		t.Fatalf("after abandon, calls = %v, want second {p1 abandoned}", calls)
	}
}

func TestMeso_ApproveOnlyForPendingApprovalTask(t *testing.T) {
	f := newFakeController()
	f.plans = []store.Plan{{ID: "p1", Title: "P", Status: store.PlanStatusActive}}
	f.tasks["p1"] = []store.Task{
		{ID: "t1", Description: "needs approval", Status: store.TaskStatusReadyPendingApproval},
		{ID: "t2", Description: "just pending", Status: store.TaskStatusPending},
	}
	u := newTestUI(t, f)
	u.selectedPlan = "p1"
	u.refreshNow()

	// There should be exactly one Approve button (for t1).
	tapButton(t, u.root, "Approve")
	approved := f.approvedTasks()
	if len(approved) != 1 || approved[0] != [2]string{"p1", "t1"} {
		t.Fatalf("ApproveTask calls = %v, want one {p1 t1}", approved)
	}
}

func TestMicro_KillButtonCallsKillWorker(t *testing.T) {
	f := newFakeController()
	f.plans = []store.Plan{{ID: "p1", Title: "P", Status: store.PlanStatusActive}}
	// The task's own claimed_by_worker_id is the authoritative kill key.
	f.tasks["p1"] = []store.Task{{ID: "t1", Description: "running task", Status: store.TaskStatusRunning, ClaimedByWorkerID: "w-123"}}
	u := newTestUI(t, f)
	u.selectedPlan = "p1"
	u.selectedTask = "t1"
	u.refreshNow()

	tapButton(t, u.root, "Kill worker")
	killed := f.killedWorkers()
	if len(killed) != 1 || killed[0] != "w-123" {
		t.Fatalf("KillWorker calls = %v, want [w-123]", killed)
	}
}

// TestMicro_KillButtonForFanoutSecondTask proves the kill affordance appears on
// a task claimed by a native-fanout worker even when it is NOT the worker's
// current_task_id (so it is absent from StatusReply.Workers): the button is
// driven by the TASK's own claimed_by_worker_id, so every task the worker holds
// gets one.
func TestMicro_KillButtonForFanoutSecondTask(t *testing.T) {
	f := newFakeController()
	f.plans = []store.Plan{{ID: "p1", Title: "P", Status: store.PlanStatusActive}}
	f.tasks["p1"] = []store.Task{
		{ID: "t1", Description: "first", Status: store.TaskStatusRunning, ClaimedByWorkerID: "w-fan"},
		{ID: "t2", Description: "second (same worker)", Status: store.TaskStatusRunning, ClaimedByWorkerID: "w-fan"},
	}
	// Only the first task appears in Workers (current_task_id), as production
	// would populate it — the second must still get a kill button.
	f.status = ipc.StatusReply{Workers: []ipc.WorkerSummary{{WorkerID: "w-fan", PlanID: "p1", TaskID: "t1"}}}

	u := newTestUI(t, f)
	u.selectedPlan = "p1"
	u.selectedTask = "t2" // the NON-first fan-out task
	u.refreshNow()

	if findButton(u.root, "Kill worker") == nil {
		t.Fatal("no kill button on a fan-out worker's non-first task")
	}
	tapButton(t, u.root, "Kill worker")
	if killed := f.killedWorkers(); len(killed) != 1 || killed[0] != "w-fan" {
		t.Errorf("KillWorker calls = %v, want [w-fan]", killed)
	}
}

func TestMicro_NoKillButtonWhenNoWorker(t *testing.T) {
	f := newFakeController()
	f.plans = []store.Plan{{ID: "p1", Title: "P", Status: store.PlanStatusActive}}
	f.tasks["p1"] = []store.Task{{ID: "t1", Description: "idle task", Status: store.TaskStatusPending}}
	// No workers in status → no kill affordance.
	u := newTestUI(t, f)
	u.selectedPlan = "p1"
	u.selectedTask = "t1"
	u.refreshNow()

	if findButton(u.root, "Kill worker") != nil {
		t.Error("kill button present when no worker is running the task")
	}
}

func TestMacro_RendersProjectEventsFeed(t *testing.T) {
	f := newFakeController()
	f.plans = []store.Plan{{ID: "p1", Title: "P", Status: store.PlanStatusActive}}
	f.pEvents = []store.Event{
		{Kind: "task.done", Actor: "worker-1"},
		{Kind: "plan.imported", Actor: "cli"},
	}
	u := newTestUI(t, f)
	u.refreshNow()

	// The macro view must show the "Recent activity" section header and at least
	// one event kind — the parity feature the TUI's macro view has.
	if !labelExists(u.root, "Recent activity") {
		t.Error("macro view missing the Recent activity header")
	}
	if !labelContains(u.root, "task.done") {
		t.Error("macro view did not render a project event (task.done)")
	}
}

func TestMacro_ProjectEventsEmptyState(t *testing.T) {
	f := newFakeController()
	f.plans = []store.Plan{{ID: "p1", Title: "P", Status: store.PlanStatusActive}}
	// no pEvents
	u := newTestUI(t, f)
	u.refreshNow()
	if !labelContains(u.root, "no activity yet") {
		t.Error("macro view should show an empty-activity state when there are no events")
	}
}

func TestMacro_EmptyStateShowsImport(t *testing.T) {
	f := newFakeController() // no plans
	u := newTestUI(t, f)
	u.refreshNow()
	if findButton(u.root, "Import plan…") == nil {
		t.Error("empty macro view should offer plan import")
	}
}

func TestTaskLabel_TruncatesOnRuneBoundary(t *testing.T) {
	// A long description made of multi-byte runes must not be sliced mid-rune
	// (which would produce invalid UTF-8). 70 emoji → truncated to 57 runes + …
	long := store.Task{Description: strings.Repeat("🚀", 70)}
	got := taskLabel(long)
	if !utf8.ValidString(got) {
		t.Fatalf("taskLabel produced invalid UTF-8: %q", got)
	}
	if r := []rune(got); len(r) != 58 { // 57 + the ellipsis
		t.Errorf("truncated label = %d runes, want 58 (57 + ellipsis)", len(r))
	}

	// A short description is returned unchanged.
	if got := taskLabel(store.Task{Description: "short"}); got != "short" {
		t.Errorf("taskLabel(short) = %q, want short", got)
	}
	// An empty description falls back to the task id.
	if got := taskLabel(store.Task{ID: "t7"}); got != "t7" {
		t.Errorf("taskLabel(empty) = %q, want the id t7", got)
	}
}
