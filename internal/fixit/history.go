package fixit

import (
	"fmt"
	"os"
	"strings"
)

// appendRefinementHistory adds a "Refinement history" section to the
// emitted plan file. Useful for operators auditing how the final
// proposal evolved across iterations.
func appendRefinementHistory(path string, refined RefineResult) error {
	if len(refined.History) <= 1 {
		// One iteration means either immediate acceptance or no
		// refinement happened — skip the history section.
		return nil
	}
	var b strings.Builder
	b.WriteString("\n## Refinement history\n\n")
	fmt.Fprintf(&b, "Fixit ran %d refinement pass(es). ", refined.Iterations)
	if refined.AcceptedAt > 0 {
		fmt.Fprintf(&b, "Pass %d was accepted.\n\n", refined.AcceptedAt)
	} else {
		fmt.Fprintln(&b, "No pass met the acceptance bar; the last attempt is reflected above.")
		b.WriteString("\n")
	}
	for _, it := range refined.History {
		marker := " "
		if it.Accepted {
			marker = "✓"
		}
		fmt.Fprintf(&b, "### Pass %d %s\n\n", it.Iteration, marker)
		fmt.Fprintf(&b, "- **Primary:** `%s`\n", it.Proposal.Primary)
		fmt.Fprintf(&b, "- **Confidence:** %d\n", it.Confidence)
		fmt.Fprintf(&b, "- **Validation:** ")
		if it.Validation.Passed {
			b.WriteString("passed\n")
		} else {
			fmt.Fprintf(&b, "%d failure(s)\n", len(it.Validation.Failures))
			for _, f := range it.Validation.Failures {
				fmt.Fprintf(&b, "  - %s\n", f)
			}
		}
		b.WriteString("\n")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec // caller-owned path
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(b.String())
	return err
}
