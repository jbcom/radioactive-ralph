//go:build gui

package gui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// The view layer is a PURE renderer: every build* function reads only the
// snapshot it is given (gathered off the main thread by gather()) and never
// performs IPC/store reads itself. That keeps all blocking work off the Fyne
// main thread — a slow socket can stale the view but never freeze it.

// headerText renders the always-visible top line. When the supervisor is
// reachable (statusErr==nil) it leads with a live "connected · up <dur>"
// indicator plus the project-wide counters; when it is not, it shows a calm
// "waiting for supervisor…" instead of just leaving stale counters — the GUI is
// designed to open before a supervisor is up and light up when one appears.
func headerText(st ipc.StatusReply, statusErr error) string {
	if statusErr != nil {
		return "waiting for supervisor…  (start one with:  radioactive_ralph service install)"
	}
	return fmt.Sprintf(
		"connected · up %s   ·   plans %d active   workers %d   running %d   ready %d   approval %d   blocked %d   failed %d",
		humanizeUptime(st.Uptime),
		st.ActivePlans, st.ActiveWorkers, st.RunningTasks, st.ReadyTasks,
		st.ApprovalTasks, st.BlockedTasks, st.FailedTasks,
	)
}

// humanizeUptime renders a supervisor uptime compactly (e.g. "3h12m", "45s").
func humanizeUptime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

// render swaps the center content to the view for the snapshot's drill level.
// Called only inside fyne.Do (main thread).
func (u *ui) render(s snapshot) {
	u.body.Objects = nil
	switch s.level {
	case levelMicro:
		u.buildMicro(s)
	case levelMeso:
		u.buildMeso(s)
	default:
		u.buildMacro(s)
	}
	u.body.Refresh()
}

// --- drill navigation: mutate the selection under the lock, then kick an async
// refresh so the next snapshot renders the new level (all reads off-thread). ---

func (u *ui) drillTo(plan, task string) {
	u.mu.Lock()
	u.selectedPlan, u.selectedTask = plan, task
	u.mu.Unlock()
	if u.syncRender {
		u.refreshNow()
		return
	}
	go u.refreshNow()
}

// statusChip is a small coloured label rendering a status in its Ralph identity
// colour — the shared status palette applied to a Fyne canvas text object.
func statusChip(status string) fyne.CanvasObject {
	t := canvas.NewText(status, statusColor(status))
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

// buildMacro lists the project's plans; selecting one drills to meso.
func (u *ui) buildMacro(s snapshot) {
	if len(s.plans) == 0 {
		u.body.Add(widget.NewLabel("No plans yet. Import a markdown plan to begin."))
		u.body.Add(u.importButton())
		return
	}
	u.body.Add(widget.NewLabelWithStyle("Plans", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	for _, p := range s.plans {
		planID := p.ID
		prog := s.progress[p.ID]
		open := widget.NewButton(p.Title, func() { u.drillTo(planID, "") })
		open.Alignment = widget.ButtonAlignLeading
		u.body.Add(container.NewHBox(
			statusChip(string(p.Status)),
			open,
			widget.NewLabel(fmt.Sprintf("%d/%d", prog.Done, prog.Total)),
		))
	}
	u.body.Add(u.importButton())
	u.addRecentActivity(s.projEvents)
}

// addRecentActivity renders the ambient project-wide event feed under the plan
// list — the GUI twin of the TUI macro view's "recent events" section.
func (u *ui) addRecentActivity(events []store.Event) {
	u.body.Add(widget.NewSeparator())
	u.body.Add(widget.NewLabelWithStyle("Recent activity", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	if len(events) == 0 {
		u.body.Add(widget.NewLabel("(no activity yet)"))
		return
	}
	for _, e := range events {
		// Newest-first (ListProjectEvents contract); show local time + kind +
		// actor, mirroring the micro timeline's format.
		u.body.Add(widget.NewLabel(fmt.Sprintf("%s  %s  %s", e.OccurredAt.Local().Format("15:04:05"), e.Kind, e.Actor)))
	}
}

// buildMeso shows one plan's tasks with per-task status, plan-level drive
// controls (pause/resume/abandon), and per-task approve.
func (u *ui) buildMeso(s snapshot) {
	planID := s.selectedPlan
	u.body.Add(u.backButton("← Plans", "", ""))

	u.body.Add(container.NewHBox(
		widget.NewButton("Pause", func() { u.drive("pause", func() error { return u.ctrl.SetPlanStatus(u.ctx, planID, "paused") }) }),
		widget.NewButton("Resume", func() { u.drive("resume", func() error { return u.ctrl.SetPlanStatus(u.ctx, planID, "active") }) }),
		widget.NewButton("Abandon", func() { u.drive("abandon", func() error { return u.ctrl.SetPlanStatus(u.ctx, planID, "abandoned") }) }),
	))

	if len(s.tasks) == 0 {
		u.body.Add(widget.NewLabel("No tasks in this plan."))
		return
	}
	for _, t := range s.tasks {
		taskID := t.ID
		open := widget.NewButton(taskLabel(t), func() { u.drillTo(planID, taskID) })
		open.Alignment = widget.ButtonAlignLeading
		row := container.NewHBox(statusChip(string(t.Status)), open)
		if t.Status == store.TaskStatusReadyPendingApproval {
			row.Add(widget.NewButton("Approve", func() {
				u.drive("approve", func() error { return u.ctrl.ApproveTask(u.ctx, planID, taskID) })
			}))
		}
		u.body.Add(row)
	}
}

// buildMicro shows one task's event timeline plus a kill affordance for the
// worker running it (when the snapshot found one).
func (u *ui) buildMicro(s snapshot) {
	planID, taskID := s.selectedPlan, s.selectedTask
	u.body.Add(u.backButton("← Tasks", planID, ""))
	u.body.Add(widget.NewLabelWithStyle("Task "+taskID, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))

	if s.killID != "" {
		killID := s.killID
		u.body.Add(widget.NewButton("Kill worker", func() {
			u.drive("kill", func() error { return u.ctrl.KillWorker(u.ctx, killID) })
		}))
	}

	if len(s.events) == 0 {
		u.body.Add(widget.NewLabel("No events yet for this task."))
		return
	}
	for _, e := range s.events {
		// Events are stored UTC; show them in the operator's local time.
		u.body.Add(widget.NewLabel(fmt.Sprintf("%s  %s  %s", e.OccurredAt.Local().Format("15:04:05"), e.Kind, e.Actor)))
	}
}

// drive runs a drive action off the main thread (it is an IPC round-trip),
// surfaces any error in the banner, and refreshes on success. Called from a tap
// handler; it spawns a goroutine so the click returns immediately.
func (u *ui) drive(label string, fn func() error) {
	work := func() {
		if err := fn(); err != nil {
			showErr := func() {
				u.errBanner.SetText(fmt.Sprintf("%s failed: %v", label, err))
				u.errBanner.Show()
			}
			if u.syncRender {
				showErr()
			} else {
				fyne.Do(showErr)
			}
			return
		}
		u.refreshNow()
	}
	if u.syncRender {
		work() // tests: run inline so the recorded drive call is immediately visible
		return
	}
	go work()
}

// backButton returns a button that drills back to (plan, task).
func (u *ui) backButton(label, plan, task string) *widget.Button {
	return widget.NewButton(label, func() { u.drillTo(plan, task) })
}

// importButton opens a small form to import a markdown plan by pasting its text.
func (u *ui) importButton() *widget.Button {
	return widget.NewButton("Import plan…", func() {
		entry := widget.NewMultiLineEntry()
		entry.SetPlaceHolder("# Plan title\n\n1. first step\n2. second step\n")
		u.body.Objects = nil
		u.body.Add(u.backButton("← Plans", "", ""))
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
	// Truncate on rune boundaries — byte-slicing could split a multi-byte
	// UTF-8 character and render as garbage.
	if r := []rune(desc); len(r) > 60 {
		desc = string(r[:57]) + "…"
	}
	if desc == "" {
		desc = t.ID
	}
	return desc
}
