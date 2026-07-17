package tui

import (
	"fmt"
	"strings"
)

// microViewportLines bounds how many lines of the log tail are shown at
// once; the rest scrolls (spec §7 "micro: one worker — its live pane / log
// tail").
const microViewportLines = 20

// renderMicro renders the single-worker/task drill-in view (spec §7
// "micro"): the selected task's stored event history (fetched once on
// drill-in) followed by any frames observed live via Attach since then,
// rendered as a scrolling tail.
func renderMicro(m Model) string {
	var b strings.Builder

	statusStr := statusStyle(string(m.selectedTask.Status)).Render(string(m.selectedTask.Status))
	b.WriteString(styleHeader.Render(fmt.Sprintf("task: %s", m.selectedTask.ID)))
	b.WriteString("\n")
	b.WriteString(styleMuted.Render(m.selectedTask.Description) + "\n")
	fmt.Fprintf(&b, "status=%s\n\n", statusStr)

	lines := microLines(m)
	view, canScrollUp, canScrollDown := windowLines(lines, m.viewport.offset, microViewportLines)

	var log strings.Builder
	if canScrollUp {
		log.WriteString(styleMuted.Render("^ more above") + "\n")
	}
	for _, l := range view {
		log.WriteString(l + "\n")
	}
	if canScrollDown {
		log.WriteString(styleMuted.Render("v more below") + "\n")
	}
	b.WriteString(styleBox.Render(strings.TrimRight(log.String(), "\n")))
	b.WriteString("\n")

	b.WriteString(renderFooter(m, "up/down: scroll   esc: back to tasks   q: quit"))
	return b.String()
}

// microLines flattens the stored task-event history and any live Attach
// frames into one chronological (oldest-first) list of renderable lines.
func microLines(m Model) []string {
	out := make([]string, 0, len(m.snap.taskEvent)+len(m.snap.live))
	// taskEvent is stored most-recent-first (ListTaskEvents contract);
	// reverse it so the whole tail reads oldest -> newest, matching how a
	// scrolling log is conventionally read.
	for i := len(m.snap.taskEvent) - 1; i >= 0; i-- {
		ev := m.snap.taskEvent[i]
		out = append(out, styleMuted.Render(ev.OccurredAt.Format("15:04:05"))+" "+ev.Kind)
	}
	for _, l := range m.snap.live {
		out = append(out, styleMuted.Render(l.at.Format("15:04:05"))+" "+l.text)
	}
	return out
}

// windowLines returns the slice of lines visible at offset (lines from the
// end, i.e. 0 = showing the newest) within height rows, plus whether more
// content exists above/below the window.
func windowLines(lines []string, offset, height int) (view []string, moreAbove, moreBelow bool) {
	if len(lines) == 0 {
		return nil, false, false
	}
	if offset < 0 {
		offset = 0
	}
	end := len(lines) - offset
	if end < 0 {
		end = 0
	}
	if end > len(lines) {
		end = len(lines)
	}
	start := end - height
	if start < 0 {
		start = 0
	}
	return lines[start:end], start > 0, offset > 0
}
