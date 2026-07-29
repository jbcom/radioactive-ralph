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
		// BANNERS AND DIAGNOSTICS. Providers print these; they are not
		// questions the agent is waiting on, and killing a turn for one is the
		// same false-positive class as the bare `permission`.
		"What's new?",
		"What went wrong?",
		"How did that happen?",
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
	//
	// Each case exercises exactly ONE pattern. The original permission example
	// -- "Claude needs permission to edit main.go. Allow this? (y/n)" -- matched
	// the permission pattern AND `allow this` AND `(y/n)`, so deleting the
	// permission pattern outright would have left this test green. A positive
	// case that several patterns satisfy proves nothing about any of them.
	mustMatch := []string{
		"Claude needs permission to edit main.go",
		// DIRECT forms. The first tightening required a verb before
		// "permission" and this|that|the after "approve", so a bare
		// "Permission to edit main.go?" and "Approve changes?" both MISSED --
		// trading false positives for false negatives, the tradeoff this
		// test's own comment warns about, because no case covered these shapes.
		"Permission to edit main.go?",
		"Approve changes?",
		"Should I use SQLite?",
		// "Allow this?" earns its place by NEGATIVE PROOF: deleting the
		// allow-this pattern outright left this suite GREEN, so nothing
		// isolated it. Every other allow-this prompt on hand also matched
		// (y/n) or "do you want to", and a case matching two patterns
		// proves neither -- the defect this very file was written to catch.
		"Allow this?",
		"Do you want to proceed?",
		"Overwrite existing file? [Y/n]",
		"Press enter to continue",
		"Which database should I target?",
		"What should I do with the migration?",
		"Where should we write the output?",
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

// TestEachPositiveCaseIsolatesOnePattern keeps the positive cases discriminating.
//
// A case matched by several patterns cannot prove any of them: the original
// permission example matched the permission pattern, `allow this`, AND `(y/n)`,
// so deleting permission detection outright left the suite green. This asserts
// the property directly, since the overlap is easy to reintroduce by making an
// example more realistic.
func TestEachPositiveCaseIsolatesOnePattern(t *testing.T) {
	for _, line := range []string{
		"Claude needs permission to edit main.go",
		// DIRECT forms. The first tightening required a verb before
		// "permission" and this|that|the after "approve", so a bare
		// "Permission to edit main.go?" and "Approve changes?" both MISSED --
		// trading false positives for false negatives, the tradeoff this
		// test's own comment warns about, because no case covered these shapes.
		"Permission to edit main.go?",
		"Approve changes?",
		"Should I use SQLite?",
		// "Allow this?" earns its place by NEGATIVE PROOF: deleting the
		// allow-this pattern outright left this suite GREEN, so nothing
		// isolated it. Every other allow-this prompt on hand also matched
		// (y/n) or "do you want to", and a case matching two patterns
		// proves neither -- the defect this very file was written to catch.
		"Allow this?",
		"Do you want to overwrite it",
		"Overwrite existing file? [Y/n]",
		"Press enter to continue",
		"Which database should I target?",
	} {
		var hits int
		for _, re := range DefaultPromptPatterns {
			if re.MatchString(line) {
				hits++
			}
		}
		if hits != 1 {
			t.Errorf("%q matches %d patterns, want exactly 1; a case several "+
				"patterns satisfy stays green when the one it was written for "+
				"is deleted", line, hits)
		}
	}
}
