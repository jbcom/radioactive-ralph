package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/jbcom/radioactive-ralph/internal/statusbucket"
)

// Palette is small and theme-neutral on purpose: the TUI runs in
// whatever terminal color scheme the operator already has, so styling
// leans on ANSI-256 codes that read reasonably against both light and
// dark backgrounds rather than hardcoded true-color hex values tuned for
// one theme.
var (
	colorAccent  = lipgloss.Color("39")  // blue: headers, selection
	colorMuted   = lipgloss.Color("244") // gray: secondary text
	colorGood    = lipgloss.Color("42")  // green: done/healthy
	colorWarn    = lipgloss.Color("214") // orange: blocked/pending-approval
	colorBad     = lipgloss.Color("203") // red: failed
	colorRunning = lipgloss.Color("81")  // cyan: running/active
)

var (
	styleHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			MarginBottom(1)

	styleSelected = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorAccent).
			SetString("> ")

	styleUnselected = lipgloss.NewStyle().SetString("  ")

	styleMuted = lipgloss.NewStyle().Foreground(colorMuted)

	styleGood    = lipgloss.NewStyle().Foreground(colorGood)
	styleWarn    = lipgloss.NewStyle().Foreground(colorWarn)
	styleBad     = lipgloss.NewStyle().Foreground(colorBad)
	styleRunning = lipgloss.NewStyle().Foreground(colorRunning)

	styleFooter = lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginTop(1)

	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorMuted).
			Padding(0, 1)
)

// statusStyle returns the style used to render a status string consistently
// across macro/meso/micro views. The status→meaning classification lives in
// internal/statusbucket (shared with the desktop GUI so a status carries the
// same meaning in both surfaces); this function only maps each semantic bucket
// to the TUI's lipgloss style. A genuinely-unknown status buckets to Muted.
func statusStyle(status string) lipgloss.Style {
	switch statusbucket.Of(status) {
	case statusbucket.Good:
		return styleGood
	case statusbucket.Running:
		return styleRunning
	case statusbucket.Warn:
		return styleWarn
	case statusbucket.Bad:
		return styleBad
	case statusbucket.Muted:
		return styleMuted
	default:
		return styleMuted
	}
}

// renderFooter renders the shared bottom help line plus any error banner,
// used by all three level renderers.
func renderFooter(m Model, help string) string {
	out := styleFooter.Render(help)
	if m.err != nil {
		out = styleBad.Render("error: "+m.err.Error()) + "\n" + out
	}
	return "\n" + out
}
