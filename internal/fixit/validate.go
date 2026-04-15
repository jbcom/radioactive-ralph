package fixit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// MinConfidence is the floor below which a plan becomes provisional
// regardless of other rules passing.
const MinConfidence = 50

// Validate runs Stage 5. Returns (passed, failures). A passing
// validation means the plan gets `status: current`; a failing
// validation still emits the plan but downgrades status to
// `provisional` so other variants' plans-first gate refuses it.
func Validate(p PlanProposal, rc RepoContext, intent IntentSpec) ValidationResult {
	var failures []string

	// Rule: primary + alternate must be known variants.
	if _, err := variant.Lookup(p.Primary); err != nil {
		failures = append(failures, fmt.Sprintf("primary %q not in variant registry", p.Primary))
	}
	if p.Alternate != "" {
		if _, err := variant.Lookup(p.Alternate); err != nil {
			failures = append(failures, fmt.Sprintf("alternate %q not in variant registry", p.Alternate))
		}
	}

	// Rule: primary's safety floors compatible with repo state.
	if prof, err := variant.Lookup(p.Primary); err == nil {
		if prof.SafetyFloors.RefuseDefaultBranch && rc.OnDefaultBranch {
			failures = append(failures, fmt.Sprintf(
				"primary %q refuses to run on the default branch, but we're on %s",
				p.Primary, rc.CurrentBranch))
		}
		if prof.HasGate() && !gateConfirmedFor(prof, intent) {
			failures = append(failures, fmt.Sprintf(
				"primary %q is gated (%s) but no matching confirmation was declared in operator constraints",
				p.Primary, prof.ConfirmationGate))
		}
	}

	// Rule: acceptance criteria contain measurable verbs only.
	allowed := regexp.MustCompile(`(?i)\b(passes|exists|matches|returns|equal|≥|≤|>=|<=|=|<|>|merge|merged|merges|resolved|closed|green|0 error|no error|100%|\d+%|fail)`)
	banned := regexp.MustCompile(`(?i)\b(improves?|considers?|addresses?|helps?|better|nicer)\b`)
	for _, crit := range p.AcceptanceCriteria {
		if banned.MatchString(crit) {
			failures = append(failures, fmt.Sprintf(
				"acceptance criterion uses banned vague verb: %q", crit))
		}
		if !allowed.MatchString(crit) {
			failures = append(failures, fmt.Sprintf(
				"acceptance criterion has no measurable verb: %q", crit))
		}
	}
	if len(p.AcceptanceCriteria) < 3 {
		failures = append(failures, fmt.Sprintf(
			"need at least 3 acceptance criteria, got %d", len(p.AcceptanceCriteria)))
	}

	// Rule: tasks that reference paths must reference real files.
	pathRe := regexp.MustCompile(`([\w./-]+\.[a-z]{1,5})`)
	for _, task := range p.Tasks {
		for _, match := range pathRe.FindAllString(task.Title, -1) {
			// Skip things like v1.2.3 and obvious non-paths.
			if !strings.ContainsAny(match, "/.") || strings.HasPrefix(match, "v") {
				continue
			}
			full := filepath.Join(rc.GitRoot, match)
			if _, err := os.Stat(full); err != nil {
				failures = append(failures, fmt.Sprintf(
					"task references non-existent file %q: %q", match, task.Title))
			}
		}
	}

	// Rule: confidence floor.
	if p.Confidence < MinConfidence {
		failures = append(failures, fmt.Sprintf(
			"confidence %d below floor %d — plan is speculative",
			p.Confidence, MinConfidence))
	}

	// Rule: operator off-limits constraints honored.
	for _, c := range intent.Constraints {
		if name, ok := strings.CutPrefix(c, "variant off-limits: "); ok {
			if p.Primary == strings.TrimSpace(name) {
				failures = append(failures, fmt.Sprintf(
					"primary %q violates operator off-limits constraint", p.Primary))
			}
		}
	}

	return ValidationResult{
		Passed:   len(failures) == 0,
		Failures: failures,
	}
}

// gateConfirmedFor reports whether the operator declared the gate
// flag in their IntentSpec.Constraints.
func gateConfirmedFor(p variant.Profile, intent IntentSpec) bool {
	needle := strings.TrimPrefix(p.ConfirmationGate, "--")
	for _, c := range intent.Constraints {
		if strings.Contains(c, needle) {
			return true
		}
	}
	return false
}
