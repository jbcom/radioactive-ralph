package tui

import (
	"fmt"
	"strings"
)

// renderMacro renders the top-level view (spec §7 "macro"): the project's
// plans with done/total progress, the overall active-worker count, and a
// tail of recent project-wide events.
func renderMacro(m Model) string {
	var b strings.Builder

	b.WriteString(styleHeader.Render("radioactive_ralph — plans"))
	b.WriteString("\n")

	// Supervisor liveness: the client only runs against a reachable supervisor,
	// so lead with "connected · up <dur>" (parity with the desktop GUI header).
	fmt.Fprintf(&b, "%s · up %s\n",
		styleGood.Render("connected"), humanizeUptime(m.snap.status.Uptime))
	fmt.Fprintf(&b, "active workers: %s   ready: %d  approval: %d  blocked: %d  running: %d  failed: %d\n\n",
		styleRunning.Render(fmt.Sprintf("%d", m.snap.status.ActiveWorkers)),
		m.snap.status.ReadyTasks, m.snap.status.ApprovalTasks,
		m.snap.status.BlockedTasks, m.snap.status.RunningTasks, m.snap.status.FailedTasks)

	if len(m.snap.plans) == 0 {
		// Actionable empty state: a bare "no plans yet" leaves the operator
		// at a dead end (the footer offers "drill into plan" with nothing to
		// drill into). Point them at the command that seeds work. Newlines
		// stay OUTSIDE Render so the style doesn't paint a trailing blank
		// styled line.
		b.WriteString(styleMuted.Render("no plans yet — import one to get started:"))
		b.WriteString("\n")
		b.WriteString(styleMuted.Render("  radioactive_ralph plan import <plan.md>"))
		b.WriteString("\n")
	}
	for i, p := range m.snap.plans {
		marker := styleUnselected.String()
		if i == m.cursor {
			marker = styleSelected.String()
		}
		prog := m.snap.progress[p.ID]
		progStr := styleMuted.Render(fmt.Sprintf("(%d/%d)", prog.Done, prog.Total))
		statusStr := statusStyle(string(p.Status)).Render(string(p.Status))
		fmt.Fprintf(&b, "%s%-30s %-10s %s\n", marker, p.Title, statusStr, progStr)
	}

	b.WriteString("\n")
	b.WriteString(styleHeader.Render("recent events"))
	b.WriteString("\n")
	if len(m.snap.planEvent) == 0 {
		b.WriteString(styleMuted.Render("(none)"))
		b.WriteString("\n")
	}
	for _, ev := range m.snap.planEvent {
		b.WriteString(styleMuted.Render(ev.OccurredAt.Format("15:04:05")) + " " + ev.Kind + "\n")
	}

	footerHint := "enter: drill into plan   q: quit"
	if len(m.snap.plans) == 0 {
		// Nothing to drill into yet — don't offer an action that does nothing.
		footerHint = "q: quit"
	}
	b.WriteString(renderFooter(m, footerHint))
	return b.String()
}
