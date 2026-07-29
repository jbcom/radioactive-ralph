package observe

import (
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestPromptKindsReachTheOperatorProjection is the check whose absence let a
// broken version ship.
//
// The first attempt at this feature specialised the failure SUMMARY inside
// provider. That summary never crossed: EventFromMetadata rebuilds a failure
// from the CATEGORY alone, and the operator DTO deliberately carries only
// FailureCategory. Every unit test passed while an operator saw no change.
//
// So this asserts the end of the pipe, not the start: each kind must produce a
// DISTINCT operator-facing summary here, where a reader actually sees it.
func TestPromptKindsReachTheOperatorProjection(t *testing.T) {
	seen := map[string]string{}
	for _, category := range []string{
		string(FailureInteractivePromptPermission),
		string(FailureInteractivePromptConfirm),
		string(FailureInteractivePromptClarification),
	} {
		ev := EventFromMetadata(store.OperatorEvent{
			Kind: "task.failed", FailureCategory: category,
		})
		if ev.Failure == nil {
			t.Errorf("category %q produced no failure summary; it falls through "+
				"the projection and an operator sees nothing", category)
			continue
		}
		if prior, dup := seen[ev.Failure.Summary]; dup {
			t.Errorf("category %q renders identically to %q; the split exists to "+
				"tell them apart", category, prior)
		}
		seen[ev.Failure.Summary] = category
	}

	// Each summary must name its remediation, which is the only reason to
	// distinguish the kinds at all.
	for summary, category := range seen {
		switch category {
		case string(FailureInteractivePromptPermission):
			if !strings.Contains(summary, "grant") {
				t.Errorf("permission summary %q does not point at a grant", summary)
			}
		case string(FailureInteractivePromptConfirm):
			if !strings.Contains(summary, "flag") {
				t.Errorf("confirm summary %q does not point at a flag", summary)
			}
		case string(FailureInteractivePromptClarification):
			if !strings.Contains(summary, "context") {
				t.Errorf("clarification summary %q does not point at the plan's context", summary)
			}
		}
	}
}
