package provider

import "regexp"

// doYouWantToPromptExpression recognizes a provider question only when the
// phrase starts a prompt-shaped line. The old unanchored expression also
// matched diagnostics and prose that merely mentioned "do you want to", which
// made the watchdog kill a working provider turn.
//
// Some CLIs omit the final question mark, so a non-punctuation character at
// end-of-line is also a prompt boundary. A terminal full stop, characteristic
// of quoted prose and diagnostics, is deliberately rejected. Internal periods
// remain valid so prompts can name files such as config.toml.
const doYouWantToPromptExpression = `(?im)^[\t ]*do you want to\b(?:[^\r\n]{0,159}\?[\t ]*|[^\r\n]{0,159}[^\s.!?][\t ]*)$`

var doYouWantToPromptPattern = regexp.MustCompile(doYouWantToPromptExpression)

// PromptKind is the CLOSED taxonomy of what an interactive block was asking
// for. It is a fixed constant, never provider text.
//
// Known permission, confirmation, and clarification shapes map to their fixed
// kinds. An interactive line with no known shape uses PromptKindUnknown; the
// generic interactive_prompt reason remains the safe fallback rather than
// guessing from provider prose.
//
// Surfacing the prompt itself is barred: only this closed set of fixed
// constants crosses to operator surfaces, never prose from an external
// process. This gives the operator the distinction without the text.
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
// first: a confirmation can contain "permission", but the permission shape
// outranks it because being asked for permission is the more actionable fact.
var promptKindPatterns = []struct {
	re   *regexp.Regexp
	kind PromptKind
}{
	{regexp.MustCompile(`(?i)permission|approve|allow this`), PromptKindPermission},
	{doYouWantToPromptPattern, PromptKindConfirm},
	{regexp.MustCompile(`(?i)\(y/n\)|\[y/n\]|continue\?|proceed\?|press enter`), PromptKindConfirm},
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
