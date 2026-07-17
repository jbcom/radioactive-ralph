//go:build gui

package gui

import (
	"context"
	"testing"

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
	f.tasks["p1"] = []store.Task{{ID: "t1", Description: "running task", Status: store.TaskStatusRunning}}
	f.status = ipc.StatusReply{
		Workers: []ipc.WorkerSummary{{PlanID: "p1", TaskID: "t1", ProviderSessionID: "w-123"}},
	}
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

func TestMacro_EmptyStateShowsImport(t *testing.T) {
	f := newFakeController() // no plans
	u := newTestUI(t, f)
	u.refreshNow()
	if findButton(u.root, "Import plan…") == nil {
		t.Error("empty macro view should offer plan import")
	}
}
