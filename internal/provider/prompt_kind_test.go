package provider

import (
	"strings"
	"testing"
)

// TestPromptKindIsAClosedTaxonomy gives an operator the one thing the current
// signal withholds, without crossing the content-safety boundary.
//
// The watchdog reports a fixed kind for known prompt shapes and retains an
// unknown fallback when it cannot classify one. A credential request and a
// routine "(y/n)" therefore remain distinguishable without exposing either
// provider line.
//
// The obvious fix is to surface the prompt text, and that is barred: only a
// CLOSED SET of fixed constants crosses to operator surfaces, never prose from
// an external process (operator_snapshot.go). BlockReason is documented
// "provider-output-free" for the same reason.
//
// A closed taxonomy respects that. The KIND is a constant Ralph derives from
// which pattern matched; no provider text travels with it.
func TestPromptKindIsAClosedTaxonomy(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want PromptKind
	}{
		{"permission request", "Claude needs permission to edit main.go", PromptKindPermission},
		{"approval request", "Do you approve this change?", PromptKindPermission},
		{"posix confirm", "Overwrite existing file? (y/n)", PromptKindConfirm},
		{"bracketed confirm", "Continue with deploy? [Y/n]", PromptKindConfirm},
		{"press enter", "Press enter to continue", PromptKindConfirm},
		{"question confirmation", "Do you want to deploy now?", PromptKindConfirm},
		{"open question", "Which database should I target?", PromptKindClarification},
		// Unmatched text must NOT be guessed at. An unknown kind is honest; a
		// wrong one sends the operator to the wrong response.
		{"unrecognized", "something entirely different", PromptKindUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyPromptKind(tc.line); got != tc.want {
				t.Errorf("ClassifyPromptKind(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

// TestDoYouWantToMentionIsNotAConfirmation keeps classification aligned with
// watchdog detection. A line the watchdog correctly ignores must not acquire a
// misleading confirm kind through a broader, second copy of the regex.
func TestDoYouWantToMentionIsNotAConfirmation(t *testing.T) {
	for _, line := range []string{
		"The phrase 'do you want to' appears in the provider output.",
		"The log records do you want to as an example.",
		"A diagnostic may say do you want to without asking.",
	} {
		if got := ClassifyPromptKind(line); got != PromptKindUnknown {
			t.Errorf("ClassifyPromptKind(%q) = %q, want %q", line, got, PromptKindUnknown)
		}
	}
}

// TestPromptKindCarriesNoProviderText is the boundary guard. A kind that
// embedded any of its input would smuggle provider prose onto an operator
// surface through the one field allowed to cross.
func TestPromptKindCarriesNoProviderText(t *testing.T) {
	const secret = "sk-proj-supersecret1234"
	got := ClassifyPromptKind("Enter your API key " + secret + " to continue")
	if string(got) == "" {
		t.Fatal("classification produced an empty kind")
	}
	if strings.Contains(string(got), secret) || strings.Contains(string(got), "API key") {
		t.Errorf("kind %q contains provider text; only fixed constants may "+
			"cross the content-safety boundary", got)
	}
}

// TestClassifiedPromptReachesTheFailureSummary is the point of the taxonomy:
// the kind has to travel to where an operator reads it.
//
// A classification that stops inside the watchdog changes nothing -- the
// operator still sees one undifferentiated interactive_prompt, which is the
// state this set out to fix. The summary is already a fixed constant, so
// specializing it by kind stays inside the content-safety boundary.
func TestClassifiedPromptReachesTheFailureSummary(t *testing.T) {
	for _, tc := range []struct {
		kind PromptKind
		want string
	}{
		{PromptKindPermission, "permission"},
		{PromptKindConfirm, "confirmation"},
		{PromptKindClarification, "clarification"},
	} {
		f := ClassifyFailure(&BlockedError{Reason: BlockReasonPrompt, Kind: tc.kind})
		// A distinct CATEGORY, not just a distinct summary. The operator
		// projection rebuilds a failure from the category alone
		// (observe.failureForEvent), so a specialised summary never crosses --
		// which is how the first version of this reached no operator at all.
		if f.Category == FailureInteractivePrompt {
			t.Errorf("kind %q kept the generic category; the projection would "+
				"render it identically to an unclassified prompt", tc.kind)
		}
		if !strings.Contains(f.Summary, tc.want) {
			t.Errorf("kind %q summary = %q, want it to name the %s so an operator "+
				"can tell a credential request from a routine y/n", tc.kind, f.Summary, tc.want)
		}
	}

	// An unclassified prompt keeps the original wording rather than inventing
	// a kind -- an unknown is honest, a guess misdirects.
	f := ClassifyFailure(&BlockedError{Reason: BlockReasonPrompt})
	if !strings.Contains(f.Summary, "interactive input") {
		t.Errorf("unclassified summary = %q, want the original generic wording", f.Summary)
	}
}

// TestClarificationIsReachableFromTheDetector closes the gap between a kind
// that CAN be classified and one that can actually occur.
//
// ClassifyPromptKind recognised open questions, but agent.Watch only calls it
// after one of DefaultPromptPatterns matches -- and none of them matched
// "Which database should I target?". So the clarification branch was
// classifiable and unreachable: a taxonomy entry nothing could produce.
func TestClarificationIsReachableFromTheDetector(t *testing.T) {
	const line = "Which database should I target?"
	var detected bool
	for _, re := range DefaultPromptPatterns {
		if re.MatchString(line) {
			detected = true
			break
		}
	}
	if !detected {
		t.Fatalf("no DefaultPromptPatterns entry matches %q, so the watchdog "+
			"never emits a Prompt signal for it and the clarification kind can "+
			"never be produced", line)
	}
	if got := ClassifyPromptKind(line); got != PromptKindClarification {
		t.Errorf("ClassifyPromptKind(%q) = %q, want clarification", line, got)
	}

	// Prose that merely CONTAINS a question word must not trip the detector --
	// a false prompt kills a healthy turn.
	for _, benign := range []string{"Running: what a great build", "how-to guide written"} {
		for _, re := range DefaultPromptPatterns {
			if re.MatchString(benign) {
				t.Errorf("%q matched a prompt pattern; a false positive kills a "+
					"turn that was working", benign)
			}
		}
	}
}
