package tui

import (
	"fmt"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/observe"
)

// renderMeso renders the drill-into-a-plan view (spec §7 "meso"): the
// selected plan's tasks grouped by their parallel_group/sequence_ordinal
// structure, their statuses, and the worker hierarchy implied by which
// tasks are currently claimed.
func renderMeso(m Model) string {
	var b strings.Builder

	prog := m.snap.progress[m.selectedPlan.ID]
	b.WriteString(styleHeader.Render(fmt.Sprintf("plan: %s", m.selectedPlan.Title)))
	b.WriteString("\n")
	b.WriteString(styleMuted.Render(fmt.Sprintf("status=%s  progress=%d/%d", m.selectedPlan.Status, prog.Done, prog.Total)))
	b.WriteString("\n\n")

	groups := groupTasks(m.snap.tasks)
	// Display policy (which partitions get a marker, and that the raw ordinal is
	// never shown) lives in observe so the TUI, GUI, and any future renderer
	// cannot drift apart on it.
	partitionLabels := observe.PartitionLabels(m.snap.tasks)
	row := 0
	for _, g := range groups {
		if g.label != "" {
			b.WriteString(styleMuted.Render(g.label) + "\n")
		}
		for _, t := range g.tasks {
			marker := styleUnselected.String()
			if row == m.cursor {
				marker = styleSelected.String()
			}
			statusStr := statusStyle(t.Status).Render(t.Status)
			worker := ""
			if t.ClaimedByWorkerID != "" {
				// Abbreviated because the worker id is a CONTROL handle -- kill
				// takes it from micro -- not something an operator reads off a scan
				// line, and at full length it was the widest marker on the row.
				//
				// The SUFFIX, not the prefix. Reading the rendered output caught
				// this: ids share a constant "worker-" head, so a leading
				// truncation printed "worker-…" on every row and correlated
				// nothing, which is the one job this marker has.
				worker = styleMuted.Render(" w:" + observe.WorkerSuffix(t.ClaimedByWorkerID, 8))
			}
			// Which provider actually ran this task. The worker id cannot answer
			// that once the work is over: worker= is a live claim, and the reaper
			// deletes worker rows while this survives in the task's own metadata,
			// so a done/failed/reaped task still reports what ran it. Shown only
			// once recorded, so a task that has not run stays visibly unassigned.
			via := ""
			if name := t.ProvenanceLabel(); name != "" {
				via = styleMuted.Render(" via=" + name)
			}
			part := ""
			if label := partitionLabels[t.PartitionOrdinal]; label != "" {
				part = styleMuted.Render(" " + label)
			}
			// The description prints in FULL. Truncating it was the first attempt
			// at fitting the width and it was wrong: a live E2E test drives the
			// real TUI and asserts an ordinary 40-character task description is
			// visible at meso, which a 24-rune cap silently clipped. That test was
			// right -- the description is what an operator reads to identify a
			// task, so the markers around it are what must give way, not it.
			fmt.Fprintf(&b, "%s%-12s %-24s %s%s%s%s\n",
				marker, t.ID, statusStr, m.snap.descriptions[t.ID], worker, via, part)
			// Why a blocked task is stalled -- the one status an operator cannot
			// act on from the status string alone, since a blocked task and one
			// waiting on a dependency both sit at zero progress and only one
			// clears itself. Blocked carries a fixed classification and static
			// remediation, never the stored error string.
			//
			// On its OWN line, because inline it pushed the worst-case row past
			// 200 columns: it wrapped on any normal terminal, and the wrap landed
			// mid-sentence so the partition marker and the reason scattered across
			// lines. Measured, not guessed -- the row is ~60 columns without it.
			// Why a failed task failed, from the durable category on the task
			// rather than the event feed: the feed is bounded and scrolls away,
			// while this survives for as long as the task stays failed. Gated
			// on status because status is the authority on what the task IS --
			// an event only explains a failure status already reports.
			if t.Status == "failed" && t.FailureCategory != "" {
				b.WriteString(styleMuted.Render(
					"               ↳ failure: " + t.FailureCategory))
				b.WriteString("\n")
			}
			// A dependency that can never be satisfied. Its own line for the
			// same reason as the block remediation: inline it pushes the row
			// past a terminal's width. Distinct from Blocked -- that is a
			// pre-dispatch configuration problem on THIS task, this is another
			// task's failure making this one unreachable.
			if t.BlockedByTaskID != "" {
				b.WriteString(styleMuted.Render(
					"               ↳ cannot run: " + t.BlockedByTaskID + " failed"))
				b.WriteString("\n")
			}
			if t.Blocked != nil && t.Blocked.Summary != "" {
				b.WriteString(styleMuted.Render("               ↳ " + t.Blocked.Summary))
				b.WriteString("\n")
			}
			row++
		}
	}
	if len(m.snap.tasks) == 0 {
		b.WriteString(styleMuted.Render("no tasks yet"))
		b.WriteString("\n")
	}
	if m.snap.tasksHasMore {
		b.WriteString(styleMuted.Render("more tasks available; showing first bounded page"))
		b.WriteString("\n")
	}

	b.WriteString(renderFooter(m, "enter: drill into task   esc: back to plans   q: quit"))
	return b.String()
}

// taskGroup is one rendering bucket in the meso view: either a
// parallel_group's tasks (label shows the group number) or the
// unsequenced/leftover bucket.
type taskGroup struct {
	label string
	tasks []observe.Task
}

// flattenGroupedTasks returns tasks in the SAME order the meso view renders
// them (ungrouped first, then each parallel group in first-seen order). The
// meso cursor indexes this order, so drillIn MUST select from it too — using
// the raw m.snap.tasks order would highlight one task but drill into another.
func flattenGroupedTasks(tasks []observe.Task) []observe.Task {
	out := make([]observe.Task, 0, len(tasks))
	for _, g := range groupTasks(tasks) {
		out = append(out, g.tasks...)
	}
	return out
}

// groupTasks buckets tasks by parallel_group for meso rendering. Tasks
// without a parallel_group render in document order under no label.
func groupTasks(tasks []observe.Task) []taskGroup {
	var ungrouped []observe.Task
	byGroup := map[int64][]observe.Task{}
	var order []int64
	seen := map[int64]bool{}

	for _, t := range tasks {
		if t.ParallelGroup == nil {
			ungrouped = append(ungrouped, t)
			continue
		}
		g := *t.ParallelGroup
		if !seen[g] {
			seen[g] = true
			order = append(order, g)
		}
		byGroup[g] = append(byGroup[g], t)
	}

	var out []taskGroup
	if len(ungrouped) > 0 {
		out = append(out, taskGroup{tasks: ungrouped})
	}
	for _, g := range order {
		out = append(out, taskGroup{label: fmt.Sprintf("group %d (parallel)", g), tasks: byGroup[g]})
	}
	return out
}
