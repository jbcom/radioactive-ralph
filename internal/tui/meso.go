package tui

import (
	"fmt"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/store"
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
			statusStr := statusStyle(string(t.Status)).Render(string(t.Status))
			worker := ""
			if t.ClaimedByWorkerID != "" {
				worker = styleMuted.Render(" worker=" + t.ClaimedByWorkerID)
			}
			fmt.Fprintf(&b, "%s%-12s %-24s %s%s\n", marker, t.ID, statusStr, t.Description, worker)
			row++
		}
	}
	if len(m.snap.tasks) == 0 {
		b.WriteString(styleMuted.Render("no tasks yet"))
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
	tasks []store.Task
}

// groupTasks buckets tasks by parallel_group for meso rendering. Tasks
// without a parallel_group render in document order under no label.
func groupTasks(tasks []store.Task) []taskGroup {
	var ungrouped []store.Task
	byGroup := map[int64][]store.Task{}
	var order []int64
	seen := map[int64]bool{}

	for _, t := range tasks {
		if !t.ParallelGroup.Valid {
			ungrouped = append(ungrouped, t)
			continue
		}
		g := t.ParallelGroup.Int64
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
