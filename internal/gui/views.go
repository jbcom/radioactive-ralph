//go:build gui

package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// headerText renders the project-wide status summary from a StatusReply — the
// always-visible top line, mirroring the TUI's macro header.
func headerText(st ipc.StatusReply) string {
	return fmt.Sprintf(
		"plans %d active   workers %d   running %d   ready %d   approval %d   blocked %d   failed %d",
		st.ActivePlans, st.ActiveWorkers, st.RunningTasks, st.ReadyTasks,
		st.ApprovalTasks, st.BlockedTasks, st.FailedTasks,
	)
}

// rebuildBody swaps the center content to the view for the current drill level:
// micro if a task is selected, meso if only a plan is, macro otherwise.
func (u *ui) rebuildBody() {
	u.body.Objects = nil
	switch {
	case u.selectedPlan != "" && u.selectedTask != "":
		u.buildMicro()
	case u.selectedPlan != "":
		u.buildMeso()
	default:
		u.buildMacro()
	}
	u.body.Refresh()
}

// statusChip is a small coloured label rendering a status in its Ralph identity
// colour — the shared status palette applied to a Fyne canvas text object.
func statusChip(status string) fyne.CanvasObject {
	t := canvas.NewText(status, statusColor(status))
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

// buildMacro lists the project's plans; selecting one drills to meso.
func (u *ui) buildMacro() {
	plans, _ := u.ctrl.ListPlans(u.ctx, u.project)
	if len(plans) == 0 {
		u.body.Add(widget.NewLabel("No plans yet. Import a markdown plan to begin."))
		u.body.Add(u.importButton())
		return
	}
	u.body.Add(widget.NewLabelWithStyle("Plans", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	for _, p := range plans {
		p := p
		prog, _ := u.ctrl.PlanProgress(u.ctx, p.ID)
		open := widget.NewButton(p.Title, func() {
			u.selectedPlan = p.ID
			u.selectedTask = ""
			u.rebuildBody()
		})
		open.Alignment = widget.ButtonAlignLeading
		row := container.NewHBox(
			statusChip(string(p.Status)),
			open,
			widget.NewLabel(fmt.Sprintf("%d/%d", prog.Done, prog.Total)),
		)
		u.body.Add(row)
	}
	u.body.Add(u.importButton())
}

// buildMeso shows one plan's tasks with per-task status, plan-level drive
// controls (pause/resume/abandon), and per-task approve.
func (u *ui) buildMeso() {
	planID := u.selectedPlan
	u.body.Add(u.backButton("← Plans", func() { u.selectedPlan = ""; u.selectedTask = "" }))

	// Plan-level controls.
	u.body.Add(container.NewHBox(
		widget.NewButton("Pause", func() { u.drive("pause", func() error { return u.ctrl.SetPlanStatus(u.ctx, planID, "paused") }) }),
		widget.NewButton("Resume", func() { u.drive("resume", func() error { return u.ctrl.SetPlanStatus(u.ctx, planID, "active") }) }),
		widget.NewButton("Abandon", func() { u.drive("abandon", func() error { return u.ctrl.SetPlanStatus(u.ctx, planID, "abandoned") }) }),
	))

	tasks, _ := u.ctrl.ListTasks(u.ctx, planID)
	if len(tasks) == 0 {
		u.body.Add(widget.NewLabel("No tasks in this plan."))
		return
	}
	for _, t := range tasks {
		t := t
		open := widget.NewButton(taskLabel(t), func() {
			u.selectedTask = t.ID
			u.rebuildBody()
		})
		open.Alignment = widget.ButtonAlignLeading
		row := container.NewHBox(statusChip(string(t.Status)), open)
		// A task awaiting approval gets an inline Approve button.
		if t.Status == store.TaskStatusReadyPendingApproval {
			taskID := t.ID
			row.Add(widget.NewButton("Approve", func() {
				u.drive("approve", func() error { return u.ctrl.ApproveTask(u.ctx, planID, taskID) })
			}))
		}
		u.body.Add(row)
	}
}

// buildMicro shows one task's event timeline plus a kill affordance for the
// worker running it.
func (u *ui) buildMicro() {
	planID, taskID := u.selectedPlan, u.selectedTask
	u.body.Add(u.backButton("← Tasks", func() { u.selectedTask = "" }))
	u.body.Add(widget.NewLabelWithStyle("Task "+taskID, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

	// Kill affordance: kill the worker currently running THIS task (looked up
	// from the live status snapshot).
	if wid := u.workerForTask(planID, taskID); wid != "" {
		u.body.Add(widget.NewButton("Kill worker", func() {
			u.drive("kill", func() error { return u.ctrl.KillWorker(u.ctx, wid) })
		}))
	}

	events, _ := u.ctrl.ListTaskEvents(u.ctx, planID, taskID, 50)
	if len(events) == 0 {
		u.body.Add(widget.NewLabel("No events yet for this task."))
		return
	}
	for _, e := range events {
		u.body.Add(widget.NewLabel(fmt.Sprintf("%s  %s  %s", e.OccurredAt.Format("15:04:05"), e.Kind, e.Actor)))
	}
}

// workerForTask returns the id of the worker running (planID,taskID) per the
// live status snapshot, or "" if none is currently running it.
func (u *ui) workerForTask(planID, taskID string) string {
	st, err := u.ctrl.Status(u.ctx)
	if err != nil {
		return ""
	}
	for _, w := range st.Workers {
		if w.PlanID == planID && w.TaskID == taskID {
			// WorkerSummary carries provider-session id, not the store worker
			// row id; the kill command keys on the worker row id, which the
			// summary exposes via ProviderSessionID only when set. Prefer an
			// explicit worker id when the summary carries one.
			if w.ProviderSessionID != "" {
				return w.ProviderSessionID
			}
		}
	}
	return ""
}

// drive runs a drive action and surfaces its error in the banner, then
// refreshes. label names the action for the error message.
func (u *ui) drive(label string, fn func() error) {
	if err := fn(); err != nil {
		fyne.Do(func() {
			u.errBanner.SetText(fmt.Sprintf("%s failed: %v", label, err))
			u.errBanner.Show()
		})
		return
	}
	u.refreshNow()
}

func (u *ui) backButton(label string, reset func()) *widget.Button {
	return widget.NewButton(label, func() {
		reset()
		u.rebuildBody()
	})
}

// importButton opens a small form to import a markdown plan by pasting its text.
func (u *ui) importButton() *widget.Button {
	return widget.NewButton("Import plan…", func() {
		entry := widget.NewMultiLineEntry()
		entry.SetPlaceHolder("# Plan title\n\n1. first step\n2. second step\n")
		u.body.Objects = nil
		u.body.Add(u.backButton("← Plans", func() { u.selectedPlan = ""; u.selectedTask = "" }))
		u.body.Add(widget.NewLabel("Paste a markdown plan:"))
		u.body.Add(entry)
		u.body.Add(widget.NewButton("Import", func() {
			u.drive("import", func() error {
				_, err := u.ctrl.ImportPlan(u.ctx, ipc.PlanImportArgs{Markdown: entry.Text, Project: u.project})
				return err
			})
		}))
		u.body.Refresh()
	})
}

func taskLabel(t store.Task) string {
	desc := t.Description
	if len(desc) > 60 {
		desc = desc[:57] + "…"
	}
	if desc == "" {
		desc = t.ID
	}
	return desc
}
