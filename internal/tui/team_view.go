package tui

import (
	"fmt"
	"strings"
)

func renderTeam(m Model) string {
	var b strings.Builder
	tasks := flattenGroupedTasks(tasksForTeam(m.snap.tasks, m.selectedTeam))
	fmt.Fprintf(&b, "%s\n", styleHeader.Render("team: "+m.selectedTeam))
	fmt.Fprintf(&b, "%s\n\n", styleMuted.Render(fmt.Sprintf(
		"plan=%s  tasks=%d", m.selectedPlan.Title, len(tasks),
	)))
	for row, task := range tasks {
		marker := styleUnselected.String()
		if row == m.cursor {
			marker = styleSelected.String()
		}
		provenance := task.AssignedAlias
		if provenance != "" {
			provenance = styleMuted.Render(
				fmt.Sprintf(" %s/%s/%s", provenance, task.AssignedModel, task.AssignedEffort),
			)
		}
		fmt.Fprintf(
			&b, "%s%-18s %-24s %s%s\n",
			marker, task.ID, statusStyle(string(task.Status)).Render(string(task.Status)),
			task.Description, provenance,
		)
	}
	if len(tasks) == 0 {
		b.WriteString(styleMuted.Render("no tasks in this team") + "\n")
	}
	b.WriteString(renderFooter(m, "enter: drill into task   esc: back to team rollups   q: quit"))
	return b.String()
}
