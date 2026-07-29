package provider

import "regexp"

// PromptKind is the CLOSED taxonomy of what an interactive block was asking
// for. It is a fixed constant, never provider text.
//
// Every interactive block currently reports one category, interactive_prompt,
// so an operator learns THAT a turn asked for something and never what kind --
// a credential request and a routine "(y/n)" look identical, though they need
// completely different responses.
//
// Surfacing the prompt itself is barred: only a closed set of fixed constants
// crosses to operator surfaces, never prose from an external process. This
// gives the operator the distinction without the text.
type PromptKind string

const (
	// PromptKindPermission is a request to be ALLOWED to act -- edit a file,
	// run a command. Usually answerable by a policy change (a wider write path,
	// a binding grant) rather than a human keystroke.
	PromptKindPermission PromptKind = "permission"
	// PromptKindConfirm is a yes/no on an action the CLI already intends. The
	// cheapest class: usually a flag that suppresses the prompt.
	PromptKindConfirm PromptKind = "confirm"
	// PromptKindClarification is an open question about the task. The
	// expensive class: it means the step's scoped context was insufficient, so
	// the PLAN needs work, not the configuration.
	PromptKindClarification PromptKind = "clarification"
	// PromptKindUnknown means no pattern matched. Deliberately not guessed:
	// a wrong kind sends the operator to the wrong response, which is worse
	// than admitting the classifier did not recognise the shape.
	PromptKindUnknown PromptKind = "unknown"
)

// promptKindPatterns map a matched shape to its kind. Ordered most-specific
// first: "do you want to" is a confirm, but "permission" anywhere in the line
// outranks it, since being asked for permission is the more actionable fact.
var promptKindPatterns = []struct {
	re   *regexp.Regexp
	kind PromptKind
}{
	{regexp.MustCompile(`(?i)permission|approve|allow this`), PromptKindPermission},
	{regexp.MustCompile(`(?i)\(y/n\)|\[y/n\]|continue\?|proceed\?|press enter|do you want to`), PromptKindConfirm},
	// Last: an open question is the residual case, and matching it early would
	// swallow confirms, which also end in "?".
	{regexp.MustCompile(`(?i)^\s*(which|what|where|how|who|should i)\b.*\?`), PromptKindClarification},
}

// ClassifyPromptKind derives the kind from a line of provider output.
//
// The INPUT is provider text; the OUTPUT is a fixed constant. That asymmetry is
// the point -- classification happens on Ralph's side of the boundary, and only
// the constant travels onward.
func ClassifyPromptKind(line string) PromptKind {
	for _, p := range promptKindPatterns {
		if p.re.MatchString(line) {
			return p.kind
		}
	}
	return PromptKindUnknown
}

// classifyPromptLine adapts ClassifyPromptKind to the watchdog's hook.
//
// The line is converted to a string HERE, inside provider, and only the
// resulting constant is returned to the agent watchdog. Nothing derived from
// the text travels back -- that asymmetry is what lets the kind reach an
// operator surface when the prompt itself never can.
func classifyPromptLine(line []byte) string {
	return string(ClassifyPromptKind(string(line)))
}
