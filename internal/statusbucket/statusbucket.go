// Package statusbucket is the single source of truth for the SEMANTIC meaning
// of a plan/task status string — "is this good, running, needs-attention, bad,
// or low-emphasis" — decoupled from any concrete rendering.
//
// Both surfaces consume it so a status can never carry a different meaning in
// the terminal than on the desktop: the TUI (internal/tui) turns a Bucket into
// a lipgloss colour, and the GUI (internal/gui) turns the same Bucket into a
// Fyne colour. Neither owns the classification; this package does. Adding a new
// store status without adding it here makes it render as the low-emphasis
// default in BOTH surfaces — the deliberate, consistent fallback.
package statusbucket

// Bucket is the semantic colour class of a status.
type Bucket int

const (
	// Muted is not-yet-started or benignly-skipped work: readable but
	// low-emphasis. Also the fallback for an unknown status.
	Muted Bucket = iota
	// Good is terminal success.
	Good
	// Running is active / in-flight.
	Running
	// Warn needs attention: blocked, awaiting approval, paused, or a partial
	// plan an operator should look at.
	Warn
	// Bad is terminal failure / abandoned.
	Bad
)

// Of maps a plan or task status string to its semantic bucket. It covers every
// real store.PlanStatus* and store.TaskStatus* value; a genuinely-unknown
// string falls through to Muted.
func Of(status string) Bucket {
	switch status {
	// Terminal-success.
	case "done":
		return Good
	// Active / in-flight.
	case "running":
		return Running
	// Needs attention (blocked on a dependency, awaiting approval, or a
	// partial/paused plan an operator should look at).
	case "blocked", "ready_pending_approval", "paused", "failed_partial":
		return Warn
	// Terminal-failure / abandoned.
	case "failed", "abandoned":
		return Bad
	// Not-yet-started or benignly-skipped work: readable but low-emphasis.
	case "pending", "ready", "draft", "skipped", "decomposed", "archived":
		return Muted
	default:
		return Muted
	}
}
