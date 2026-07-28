package orch

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestFanoutPathEnforcesEveryAdmissionRule is a STRUCTURAL guard against a gap
// that has now recurred three times.
//
// DispatchNext's native-fan-out branch returns BEFORE the per-step admission
// loop, so every rule added to that loop is invisible to fan-out unless it is
// restated. `providers` and `differentFrom` were both silently bypassed that
// way (fixed #272), and containment admission was the third -- a whole parallel
// group would have run unconfined while the project requested a boundary.
//
// Each time, the rule was correct, tested, and simply not applied on one path.
// A source-level check is the only kind that catches the FOURTH instance,
// because a behavioral test only exists once someone has already thought of the
// case.
//
// This asserts the rules are PRESENT on the fan-out path, which is deliberately
// weaker than asserting they behave identically -- the paths legitimately
// differ (one worker for N tasks). It fails loudly when a new admission rule is
// added per-step and forgotten here, which is the actual recurring mistake.
func TestFanoutPathEnforcesEveryAdmissionRule(t *testing.T) {
	src, err := os.ReadFile("orchestrator.go")
	if err != nil {
		t.Fatalf("read orchestrator.go: %v", err)
	}
	text := string(src)

	perStep := funcBody(t, text, "func (o *Orchestrator) dispatchReadyStep")
	fanoutBranch := fanoutRegion(t, text)
	fanoutGroup := funcBody(t, text, "func (o *Orchestrator) dispatchFanoutGroup")
	coalescable := funcBody(t, text, "func (o *Orchestrator) coalescableSteps")
	fanoutAll := fanoutBranch + fanoutGroup + coalescable

	// Each rule, and where the fan-out path is allowed to satisfy it. Several
	// are enforced by EXCLUSION (coalescableSteps drops the step from the group)
	// rather than by refusal, which is correct: one turn cannot honour a
	// per-step binding restriction, so the step goes to the per-step loop.
	rules := []struct {
		name   string
		needle string
	}{
		{"provider restriction (`providers`)", "AllowedProviders()"},
		{"independence (`differentFrom`)", "IndependencePeers()"},
		{"binding pin (`binding.provider`)", "PinnedProviderType()"},
		{"write containment", "resolveContainment("},
		{"spend cap", "checkSpendCap("},
	}
	for _, r := range rules {
		if !strings.Contains(perStep, r.needle) {
			t.Fatalf("%s (%s) is no longer in dispatchReadyStep; this test's premise "+
				"is stale — update it deliberately rather than deleting the check",
				r.name, r.needle)
		}
		if !strings.Contains(fanoutAll, r.needle) {
			t.Errorf("%s is enforced per-step (%s) but appears NOWHERE on the fan-out "+
				"path (the fan-out branch, dispatchFanoutGroup, or coalescableSteps).\n"+
				"The fan-out branch returns before the per-step loop, so this rule does "+
				"not apply to a coalesced group — either restate it there or exclude the "+
				"affected steps from coalescing.", r.name, r.needle)
		}
	}
}

// funcBody returns the source of the function whose declaration starts with
// decl, from the declaration to the first line that is exactly "}".
func funcBody(t *testing.T, src, decl string) string {
	t.Helper()
	i := strings.Index(src, decl)
	if i < 0 {
		t.Fatalf("declaration not found: %q — this test reads source, so a rename "+
			"breaks it by design rather than silently passing", decl)
	}
	rest := src[i:]
	if j := strings.Index(rest, "\n}\n"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// fanoutRegion returns the NativeFanout branch inside DispatchNext: the region
// that returns before the per-step admission loop.
func fanoutRegion(t *testing.T, src string) string {
	t.Helper()
	start := regexp.MustCompile(`if binding\.Config\.NativeFanout \{`).FindStringIndex(src)
	if start == nil {
		t.Fatal("NativeFanout branch not found in DispatchNext")
	}
	rest := src[start[0]:]
	// The branch ends at the per-step loop that follows it.
	if j := strings.Index(rest, "for i := 0; i < candidateLimit; i++ {"); j > 0 {
		return rest[:j]
	}
	return rest
}
