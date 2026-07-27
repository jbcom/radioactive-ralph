package tui

import (
	"fmt"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/observe"
)

// renderMacro renders the top-level view (spec §7 "macro"): the project's
// plans with done/total progress, the overall active-worker count, and a
// tail of recent project-wide events.
func renderMacro(m Model) string {
	var b strings.Builder

	b.WriteString(styleHeader.Render("radioactive_ralph — plans"))
	b.WriteString("\n")

	// Supervisor liveness. On a healthy fetch, "connected · up <dur>" (parity
	// with the desktop GUI header). But if the last refresh FAILED (supervisor
	// exited or became unreachable mid-session), the status snapshot is stale —
	// don't keep claiming "connected" with a frozen uptime; show the real state.
	if m.err != nil {
		b.WriteString(styleBad.Render("disconnected") + styleMuted.Render(" — retrying…") + "\n")
	} else {
		fmt.Fprintf(&b, "%s · observed %s\n",
			styleGood.Render("connected"), m.snap.capturedAt.Local().Format("15:04:05"))
	}
	fmt.Fprintf(&b, "active workers: %s   ready: %d  approval: %d  blocked: %d  running: %d  failed: %d\n\n",
		styleRunning.Render(fmt.Sprintf("%d", m.snap.summary.ActiveWorkerCount)),
		summaryStatusCount(m.snap.summary.TaskStatusCounts, "ready"),
		summaryStatusCount(m.snap.summary.TaskStatusCounts, "ready_pending_approval"),
		summaryStatusCount(m.snap.summary.TaskStatusCounts, "blocked"),
		summaryStatusCount(m.snap.summary.TaskStatusCounts, "running"),
		summaryStatusCount(m.snap.summary.TaskStatusCounts, "failed"))

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
		statusStr := statusStyle(p.Status).Render(p.Status)
		fmt.Fprintf(&b, "%s%-30s %-10s %s\n", marker, p.Title, statusStr, progStr)
	}
	if m.snap.plansHasMore {
		b.WriteString(styleMuted.Render("(more plans available; showing first bounded page)"))
		b.WriteString("\n")
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
	if m.snap.eventsHasMore {
		b.WriteString(styleMuted.Render("(older events available)"))
		b.WriteString("\n")
	}

	footerHint := "enter: drill into plan   q: quit"
	if len(m.snap.plans) == 0 {
		// Nothing to drill into yet — don't offer an action that does nothing.
		footerHint = "q: quit"
	}
	b.WriteString(renderFooter(m, footerHint))
	return b.String()
}

func summaryStatusCount(counts []observe.StatusCount, status string) int {
	for _, count := range counts {
		if count.Status == status {
			return count.Count
		}
	}
	return 0
}
