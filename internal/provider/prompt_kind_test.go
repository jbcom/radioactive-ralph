package provider

import (
	"strings"
	"testing"
)

// TestPromptKindIsAClosedTaxonomy gives an operator the one thing the current
// signal withholds, without crossing the content-safety boundary.
//
// Today every interactive block reports the same category: interactive_prompt.
// An operator learns THAT the turn asked for something, never what KIND -- so a
// credential request and a routine "(y/n)" are indistinguishable, and the two
// need completely different responses.
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
		if f.Category != FailureInteractivePrompt {
			t.Errorf("kind %q gave category %q, want interactive_prompt", tc.kind, f.Category)
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
