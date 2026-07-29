package agent

import (
	"regexp"
	"testing"
)

// TestWatchdogConfigCarriesTheClassifiedKind covers the TRANSPORT, which no
// other test does.
//
// provider owns the taxonomy and agent owns the match loop, so the kind has to
// cross a package boundary on the Signal. Both sides are tested in isolation
// -- provider proves the classification, observe proves the projection -- and
// neither would notice if the value were dropped in between. That is exactly
// the shape of defect this change already produced twice: correct pieces, no
// effect.
func TestWatchdogConfigCarriesTheClassifiedKind(t *testing.T) {
	cfg := WatchdogConfig{
		PromptPatterns: []*regexp.Regexp{regexp.MustCompile(`(?i)permission`)},
		ClassifyPrompt: func(line []byte) string {
			if string(line) == "needs permission" {
				return "permission"
			}
			return "unknown"
		},
	}
	if got := cfg.promptKind([]byte("needs permission")); got != "permission" {
		t.Errorf("promptKind = %q, want the classifier's answer to reach the "+
			"signal; a dropped kind leaves every block undifferentiated", got)
	}

	// A nil classifier must be silent rather than panicking: the hook is
	// optional, and every existing caller predates it.
	bare := WatchdogConfig{PromptPatterns: cfg.PromptPatterns}
	if got := bare.promptKind([]byte("needs permission")); got != "" {
		t.Errorf("promptKind with no classifier = %q, want empty", got)
	}
}
