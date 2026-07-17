//go:build gui

package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"

	"github.com/jbcom/radioactive-ralph/internal/statusbucket"
)

// Ralph's identity palette as true-color values. These are the desktop twins of
// the TUI's ANSI palette (internal/tui/styles.go): the SAME semantic meanings,
// rendered in hex so the desktop reads as the same product as the terminal.
// CONSISTENT, not native — this is a fixed Ralph identity, not the OS theme.
var (
	ralphAccent  = color.NRGBA{R: 0x26, G: 0x8b, B: 0xd2, A: 0xff} // blue: headers/selection
	ralphMuted   = color.NRGBA{R: 0x83, G: 0x94, B: 0x96, A: 0xff} // gray: low-emphasis
	ralphGood    = color.NRGBA{R: 0x2a, G: 0xa1, B: 0x98, A: 0xff} // green: done/healthy
	ralphWarn    = color.NRGBA{R: 0xcb, G: 0x8b, B: 0x1a, A: 0xff} // orange: needs attention
	ralphBad     = color.NRGBA{R: 0xdc, G: 0x32, B: 0x2f, A: 0xff} // red: failed/abandoned
	ralphRunning = color.NRGBA{R: 0x22, G: 0xb2, B: 0xb2, A: 0xff} // cyan: running/active
)

// bucketColor turns a semantic status bucket into its Ralph identity colour.
// Both the TUI and the GUI derive their colour from statusbucket.Of, so a
// status carries the same meaning in both; this is the GUI's rendering half.
func bucketColor(b statusbucket.Bucket) color.Color {
	switch b {
	case statusbucket.Good:
		return ralphGood
	case statusbucket.Running:
		return ralphRunning
	case statusbucket.Warn:
		return ralphWarn
	case statusbucket.Bad:
		return ralphBad
	case statusbucket.Muted:
		return ralphMuted
	default:
		return ralphMuted
	}
}

// statusColor is the convenience the views call: status string → identity colour.
func statusColor(status string) color.Color { return bucketColor(statusbucket.Of(status)) }

// ralphTheme is Ralph's fixed visual identity for the desktop client. It embeds
// Fyne's default theme for sizing/fonts/icons and overrides only the primary
// (accent) colour so selection and emphasis read as Ralph blue regardless of
// the OS light/dark setting. Per-status colours are applied directly by the
// views via statusColor, not through the theme's colour names (Fyne has no
// "task-failed" colour name), so the theme stays small and the status palette
// stays the single source shared with the TUI.
type ralphTheme struct{}

var _ fyne.Theme = ralphTheme{}

func (ralphTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNamePrimary {
		return ralphAccent
	}
	return theme.DefaultTheme().Color(name, variant)
}

func (ralphTheme) Font(s fyne.TextStyle) fyne.Resource    { return theme.DefaultTheme().Font(s) }
func (ralphTheme) Icon(n fyne.ThemeIconName) fyne.Resource { return theme.DefaultTheme().Icon(n) }
func (ralphTheme) Size(n fyne.ThemeSizeName) float32       { return theme.DefaultTheme().Size(n) }
