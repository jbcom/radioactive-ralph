//go:build gui

package gui

import (
	"fmt"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/observe"
)

// The view layer is a PURE renderer: every build* function reads only the
// snapshot it is given (gathered off the main thread by gather()) and never
// performs IPC/store reads itself. That keeps all blocking work off the Fyne
// main thread — a slow socket can stale the view but never freeze it.

// headerText renders the always-visible top line. When the supervisor is
// reachable (statusErr==nil) it leads with a live "connected · up <dur>"
// indicator plus the counters; when it is not, it shows a calm "waiting for
// supervisor…" instead of leaving stale counters — the GUI is designed to open
// before a supervisor is up and light up when one appears.
//
// The counters come from the supervisor-wide StatusReply (the supervisor is
// project-agnostic), whereas the plan list below is scoped to the launching
// project — so the header explicitly labels the counts "all projects" to avoid
// the operator trying to reconcile them with the visible per-project rows.
func headerText(
	summary observe.Summary,
	capturedAt time.Time,
	statusErr error,
) string {
	if statusErr != nil {
		return "waiting for supervisor…  (" + noSupervisorHintFor(runtime.GOOS) + ")"
	}
	return fmt.Sprintf(
		"connected · observed %s   ·   project: plans %d   workers %d   running %d   ready %d   approval %d   blocked %d   failed %d",
		capturedAt.Local().Format("15:04:05"),
		summary.PlanTotal,
		summary.ActiveWorkerCount,
		guiStatusCount(summary.TaskStatusCounts, "running"),
		guiStatusCount(summary.TaskStatusCounts, "ready"),
		guiStatusCount(summary.TaskStatusCounts, "ready_pending_approval"),
		guiStatusCount(summary.TaskStatusCounts, "blocked"),
		guiStatusCount(summary.TaskStatusCounts, "failed"),
	)
}

func guiStatusCount(counts []observe.StatusCount, status string) int {
	for _, count := range counts {
		if count.Status == status {
			return count.Count
		}
	}
	return 0
}

func noSupervisorHintFor(goos string) string {
	if goos == "windows" {
		return "start the native control plane with: radioactive_ralph --supervisor; " +
			"provider PTYs require WSL2 with systemd --user"
	}
	return "start one with:  radioactive_ralph service install"
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
// Called only inside the UI paint dispatcher (main thread).
func (u *ui) render(s snapshot) {
	u.body.Objects = nil
	u.firstFocusable = nil
	switch s.level {
	case levelMicro:
		u.buildMicro(s)
	case levelMeso:
		u.buildMeso(s)
	default:
		u.buildMacro(s)
	}
	u.body.Refresh()
	// Land keyboard focus on the first actionable control so a keyboard-only
	// operator can act on arrival without blind-Tabbing — but ONLY when the drill
	// view just changed. render() also runs on every 1s tick and live event; if we
	// focused unconditionally, each refresh would yank focus back to the first
	// control, stealing it from an operator Tabbing toward Pause/Approve/Kill. So
	// (re)initialize focus only when the view identity changes, and otherwise leave
	// the operator's current focus untouched during ordinary data refreshes.
	viewID := fmt.Sprintf("%d\x00%s\x00%s", s.level, s.selectedPlan, s.selectedTask)
	if viewID != u.focusedView {
		u.focusedView = viewID
		if c := u.win.Canvas(); c != nil {
			c.Focus(u.firstFocusable) // Focus(nil) is a safe no-op (blurs)
		}
		// Reset the scroll offset on a new view: drilling in from a scrolled-down
		// list, or back out, should land at the top of the new content, not at
		// whatever offset the previous level happened to be at.
		if u.scroll != nil {
			u.scroll.ScrollToTop()
		}
	}
}

// button builds a labeled button and records it as the view's first focusable
// control if none has been recorded yet this render — so render() can land
// keyboard focus on the first actionable widget after every rebuild.
func (u *ui) button(label string, tapped func()) *widget.Button {
	b := widget.NewButton(label, tapped)
	if u.firstFocusable == nil {
		u.firstFocusable = b
	}
	return b
}

// --- drill navigation: mutate the selection under the lock, then kick an async
// refresh so the next snapshot renders the new level (all reads off-thread). ---

func (u *ui) drillTo(plan, task string) {
	u.mu.Lock()
	u.selectedPlan, u.selectedTask = plan, task
	u.actionErr = "" // a prior view's action error must not follow the operator here
	u.importing = false
	u.viewToken++ // invalidate any in-flight drive issued from the prior view
	u.mu.Unlock()
	if u.syncRender {
		u.refreshNow()
		return
	}
	u.goAsync(u.refreshNow)
}

// drillBack navigates up one level (micro→meso→macro), the keyboard (Escape)
// equivalent of the on-screen back buttons. A no-op at macro.
func (u *ui) drillBack() {
	u.mu.Lock()
	switch {
	case u.selectedTask != "":
		u.selectedTask = "" // micro → meso
	case u.selectedPlan != "":
		u.selectedPlan = "" // meso → macro
	default:
		u.mu.Unlock()
		return // already at macro
	}
	u.actionErr = "" // clear a prior view's action error when leaving it
	u.importing = false
	u.viewToken++ // invalidate any in-flight drive issued from the view we left
	u.mu.Unlock()
	if u.syncRender {
		u.refreshNow()
		return
	}
	u.goAsync(u.refreshNow)
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
	// Import needs a project context — the supervisor rejects a plan-import with
	// an empty project id. A project-agnostic launch (no project scope) is
	// read-only for import; only offer the affordance when scoped to a project.
	canImport := u.project != ""

	if len(s.plans) == 0 {
		if canImport {
			u.body.Add(widget.NewLabel("No plans yet. Import a markdown plan to begin."))
			u.body.Add(u.importButton())
		} else {
			u.body.Add(widget.NewLabel("No active plans. Launch from a project directory to import one."))
		}
		// The activity feed is still worth showing with zero plans (the TUI does
		// too) — a fresh project may have supervisor/service events before its
		// first plan. Fall through to addRecentActivity rather than returning.
	} else {
		u.body.Add(widget.NewLabelWithStyle("Plans", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
		for _, p := range s.plans {
			planID := p.ID
			prog := s.progress[p.ID]
			open := u.button(p.Title, func() { u.drillTo(planID, "") })
			open.Alignment = widget.ButtonAlignLeading
			u.body.Add(container.NewHBox(
				statusChip(string(p.Status)),
				open,
				widget.NewLabel(fmt.Sprintf("%d/%d", prog.Done, prog.Total)),
			))
		}
		if canImport {
			u.body.Add(u.importButton())
		}
	}
	if s.plansHasMore {
		u.body.Add(widget.NewLabel(
			"More plans available; showing the first bounded page.",
		))
	}
	u.addRecentActivity(s.projEvents)
}

// addRecentActivity renders the ambient project-wide event feed under the plan
// list — the GUI twin of the TUI macro view's "recent events" section.
func (u *ui) addRecentActivity(events []observe.Event) {
	u.body.Add(widget.NewSeparator())
	u.body.Add(widget.NewLabelWithStyle("Recent activity", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	if len(events) == 0 {
		u.body.Add(widget.NewLabel("(no activity yet)"))
		return
	}
	for _, e := range events {
		u.body.Add(widget.NewLabel(safeEventLabel(e)))
	}
}

func safeEventLabel(event observe.Event) string {
	label := fmt.Sprintf(
		"%s  %s",
		event.OccurredAt.Local().Format("15:04:05"),
		event.Kind,
	)
	if event.Failure != nil {
		label += "  failure=" + string(event.Failure.Category)
	}
	return label
}

// buildMeso shows one plan's tasks with per-task status, plan-level drive
// controls (pause/resume/abandon), and per-task approve.
func (u *ui) buildMeso(s snapshot) {
	planID := s.selectedPlan
	u.body.Add(u.backButton("← Plans", "", ""))

	u.body.Add(container.NewHBox(
		widget.NewButton("Pause", func() { u.drive("pause", func() error { return u.ctrl.SetPlanStatus(u.ctx, planID, "paused") }) }),
		widget.NewButton("Resume", func() { u.drive("resume", func() error { return u.ctrl.SetPlanStatus(u.ctx, planID, "active") }) }),
		widget.NewButton("Abandon", func() {
			u.confirmDrive("Abandon plan?",
				"Abandon plan "+planID+"? No further tasks will be dispatched. (Tasks already running finish; you can set it active again to resume dispatch.)",
				"abandon", func() error { return u.ctrl.SetPlanStatus(u.ctx, planID, "abandoned") })
		}),
	))

	if len(s.tasks) == 0 {
		u.body.Add(widget.NewLabel("No tasks in this plan."))
		return
	}
	// Only partitions holding more than one task are labelled: a partition of
	// one is the ordinary case, so marking every row would bury the fan-out
	// groups the marker exists to reveal. Numbered per view because the ordinal
	// itself is an unreadable hash — the operator needs "these two rows are one
	// turn", never the digest.
	partitionSize := map[string]int{}
	for _, t := range s.tasks {
		if t.PartitionOrdinal != "" {
			partitionSize[t.PartitionOrdinal]++
		}
	}
	partitionLabels := map[string]string{}
	for _, t := range s.tasks {
		if partitionSize[t.PartitionOrdinal] < 2 {
			continue
		}
		if _, seen := partitionLabels[t.PartitionOrdinal]; !seen {
			partitionLabels[t.PartitionOrdinal] = fmt.Sprintf("p%d", len(partitionLabels)+1)
		}
	}

	for _, t := range s.tasks {
		taskID := t.ID
		open := u.button(taskLabel(t), func() { u.drillTo(planID, taskID) })
		open.Alignment = widget.ButtonAlignLeading
		row := container.NewHBox(statusChip(string(t.Status)), open)
		// Provenance rides beside the button rather than inside taskLabel: that
		// label is the task's identity (and the drill target's name), so folding
		// a provider into it would make the same task read differently before
		// and after it runs. Omitted entirely when unrecorded, so an
		// undispatched task never displays a provider it did not use.
		if name := t.ProvenanceLabel(); name != "" {
			row.Add(widget.NewLabel("via " + name))
		}
		if label := partitionLabels[t.PartitionOrdinal]; label != "" {
			row.Add(widget.NewLabel(label))
		}
		if t.Status == "ready_pending_approval" {
			row.Add(widget.NewButton("Approve", func() {
				u.drive("approve", func() error { return u.ctrl.ApproveTask(u.ctx, planID, taskID) })
			}))
		}
		u.body.Add(row)
	}
	if s.tasksHasMore {
		u.body.Add(widget.NewLabel(
			"More tasks available; showing the first bounded page.",
		))
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
			u.confirmDrive("Kill worker?",
				"Kill worker "+killID+"? Its running task is interrupted and requeued.",
				"kill", func() error { return u.ctrl.KillWorker(u.ctx, killID) })
		}))
	}

	if len(s.events) == 0 {
		u.body.Add(widget.NewLabel("No events yet for this task."))
		return
	}
	for _, e := range s.events {
		u.body.Add(widget.NewLabel(safeEventLabel(e)))
	}
	if s.eventsHasMore {
		u.body.Add(widget.NewLabel("Older events are available."))
	}
}

// drive runs a drive action off the main thread (it is an IPC round-trip) and
// refreshes to surface the result. Called from a tap handler; it spawns a
// goroutine so the click returns immediately. Both the success and failure paths
// go through refreshNow → paint: a failure records actionErr (rendered with
// precedence and persisting across refreshes until cleared), and a success clears
// actionErr and repaints the fresh state. Routing errors through paint (rather
// than a bare fyne.Do banner write) keeps them coordinated with the refreshSeq
// staleness gate so a stale tick can neither erase a fresh error nor resurrect a
// cleared one.
func (u *ui) drive(label string, fn func() error) {
	u.mu.Lock()
	token := u.viewToken // the view this action was issued from
	u.mu.Unlock()
	work := func() {
		err := fn()
		u.mu.Lock()
		// Drop the outcome if the operator drilled away while the RPC was in
		// flight: a late completion must not resurrect a banner on — or clear the
		// state of — the view they moved to.
		if u.viewToken != token {
			u.mu.Unlock()
			return
		}
		if err != nil {
			u.actionErr = fmt.Sprintf("%s failed: %v", label, err)
		} else {
			u.actionErr = ""
		}
		u.importing = false // an action leaves the import form
		u.mu.Unlock()
		u.refreshNow()
	}
	if u.syncRender {
		work() // tests: run inline so the recorded drive call is immediately visible
		return
	}
	u.goAsync(work)
}

// confirmDrive gates a drive behind a modal yes/no confirmation — for the
// irreversible one-click actions (abandon a plan, kill a running worker) where a
// stray click has no undo. `prompt` should name exactly what will happen,
// including any id (e.g. the worker being killed), so the operator can verify
// before committing. Under the headless test driver (syncRender) the dialog is
// skipped and the drive runs directly, since that driver can't dismiss a modal.
func (u *ui) confirmDrive(title, prompt, label string, fn func() error) {
	if u.syncRender {
		u.drive(label, fn)
		return
	}
	dialog.ShowConfirm(title, prompt, func(ok bool) {
		if ok {
			u.drive(label, fn)
		}
	}, u.win)
}

// backButton returns a button that drills back to (plan, task). It routes
// through u.button so that at meso/micro (where the back button is added first)
// keyboard focus lands here after a rebuild.
func (u *ui) backButton(label, plan, task string) *widget.Button {
	return u.button(label, func() { u.drillTo(plan, task) })
}

// importButton opens a small form to import a markdown plan by pasting its text.
func (u *ui) importButton() *widget.Button {
	return u.button("Import plan…", func() {
		// Mark that the imperative import form is up so the periodic paint stops
		// rebuilding the body (which would wipe the form and any pasted text). The
		// back button and a successful/failed import clear it via drill/drive.
		u.mu.Lock()
		u.importing = true
		u.mu.Unlock()
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

func taskLabel(task observe.Task) string {
	if task.ID != "" {
		return task.ID
	}
	return task.CanonicalID
}
