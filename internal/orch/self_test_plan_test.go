package orch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	plan2 "github.com/jbcom/radioactive-ralph/internal/plan"
)

// selfTestPlanPath is the plan scripts/self-test.sh imports.
const selfTestPlanPath = "../../docs/plans/self-test.md"

// TestSelfTestPlanIsImportable keeps the dogfooding plan from rotting.
//
// Nothing else exercises it: it is markdown, not code, so a malformed
// ralph-task block, a dependency naming a task that does not exist, or a
// mistyped acceptance marker would all survive every other test in the repo and
// only surface when someone ran the self-test by hand and watched it die.
//
// That matters more than it sounds, because a broken plan fails the SAME way a
// broken product does -- tasks that never complete -- which is exactly the
// ambiguity the self-test exists to remove.
func TestSelfTestPlanIsImportable(t *testing.T) {
	md, err := os.ReadFile(filepath.Clean(selfTestPlanPath))
	if err != nil {
		t.Fatalf("read self-test plan: %v", err)
	}
	p, err := plan2.Parse(md)
	if err != nil {
		t.Fatalf("the shipped self-test plan does not parse: %v", err)
	}
	if len(p.Groups) == 0 {
		t.Fatal("the self-test plan decomposed to zero groups; it would import " +
			"successfully and then verify nothing")
	}
}

// TestSelfTestPlanStepsAllCarryAcceptance is the property that makes the plan a
// TEST rather than a script that reports success for having run.
//
// A step with no `accept:` marker is judgment-only: accepted on any non-empty
// worker evidence. The first version of this plan had none, so every task was
// unverifiable and the whole run died -- and the failure looked like a product
// bug rather than a plan bug.
func TestSelfTestPlanStepsAllCarryAcceptance(t *testing.T) {
	md, err := os.ReadFile(filepath.Clean(selfTestPlanPath))
	if err != nil {
		t.Fatalf("read self-test plan: %v", err)
	}
	p, err := plan2.Parse(md)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var steps int
	for _, g := range p.Groups {
		for _, step := range g.Steps {
			steps++
			acc, err := defaultAcceptanceJSON(step)
			if err != nil {
				t.Fatalf("derive acceptance for %q: %v", step.Text, err)
			}
			if acc == "" {
				t.Errorf("step %q carries no acceptance marker, so completion "+
					"would be accepted on worker evidence alone -- it verifies "+
					"nothing", strings.TrimSpace(step.Text))
			}
		}
	}
	if steps == 0 {
		t.Fatal("no steps found; this test would assert nothing")
	}
}
