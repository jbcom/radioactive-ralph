package provider

import "testing"

// TestPromptPatternsRejectErrorText fixes a false positive that has been
// mislabelling ordinary failures as blocked turns.
//
// DefaultPromptPatterns matched a bare `(?i)permission`, so "permission
// denied" -- an ERROR, not a question -- killed the turn and reported
// interactive_prompt. The CLI was never waiting for anybody.
//
// It went unnoticed because the category was undifferentiated: every block
// looked alike, so "the agent asked for input" was never checked against "the
// agent printed an error containing the word permission". Naming the kind is
// what made it legible.
//
// The cost is asymmetric, which is why the patterns should err toward missing
// a prompt: a false NEGATIVE stalls one turn until the lease expires, while a
// false POSITIVE kills a working turn AND misdirects the diagnosis.
func TestPromptPatternsRejectErrorText(t *testing.T) {
	mustNotMatch := []string{
		// The exact strings that misfired.
		"permission denied",
		"Error: permission denied writing /tmp/x",
		"you do not have permission to write that file",
		"checking file permissions",
		// Neighbours in the same family.
		"approved by the linter",
		"the operation was not allowed this time",
	}
	for _, line := range mustNotMatch {
		for _, re := range DefaultPromptPatterns {
			if re.MatchString(line) {
				t.Errorf("%q matched %v -- error text detected as a prompt, which "+
					"kills a working turn and reports it as interactive_prompt",
					line, re)
			}
		}
	}

	// Real prompts must still be caught: over-tightening trades one silent
	// failure for another.
	mustMatch := []string{
		"Claude needs permission to edit main.go. Allow this? (y/n)",
		"Do you want to proceed?",
		"Overwrite existing file? [Y/n]",
		"Press enter to continue",
		"Which database should I target?",
	}
	for _, line := range mustMatch {
		var hit bool
		for _, re := range DefaultPromptPatterns {
			if re.MatchString(line) {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("%q matched NO prompt pattern; a real block would hang until "+
				"the stall lease expires", line)
		}
	}
}
