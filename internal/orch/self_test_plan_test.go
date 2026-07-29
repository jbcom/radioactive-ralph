package orch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/plan"
)

// selfTestPlanPath is the plan scripts/self-test.sh imports.
const selfTestPlanPath = "../../docs/plans/self-test.md"

func readSelfTestPlan(t *testing.T) *plan.Plan {
	t.Helper()
	md, err := os.ReadFile(filepath.Clean(selfTestPlanPath))
	if err != nil {
		t.Fatalf("read self-test plan: %v", err)
	}
	p, err := plan.Parse(md)
	if err != nil {
		t.Fatalf("the shipped self-test plan does not parse: %v", err)
	}
	return p
}

// walkSteps visits every step in the plan, including those under NESTED
// headings. A Group carries either Steps or SubGroups and the importer walks
// both, so a check that inspected only top-level groups would be looking at a
// subset of what actually runs -- and would call a plan clean while a nested
// heading introduced an unverified step.
func walkSteps(groups []plan.Group, visit func(plan.Step)) {
	for _, g := range groups {
		walkSteps(g.SubGroups, visit)
		for _, s := range g.Steps {
			visit(s)
		}
	}
}

// TestSelfTestPlanIsImportable keeps the dogfooding plan from rotting.
//
// Nothing else exercises it: it is markdown, not code, so a malformed
// ralph-task block or a mistyped acceptance marker would survive every other
// test in the repo and surface only when someone ran the self-test by hand and
// watched it die.
//
// That matters more than it sounds, because a broken PLAN fails the same way a
// broken PRODUCT does -- tasks that never complete -- which is exactly the
// ambiguity the self-test exists to remove.
func TestSelfTestPlanIsImportable(t *testing.T) {
	p := readSelfTestPlan(t)
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
	p := readSelfTestPlan(t)

	var steps int
	walkSteps(p.Groups, func(step plan.Step) {
		steps++
		acc, err := defaultAcceptanceJSON(step)
		if err != nil {
			t.Fatalf("derive acceptance for %q: %v", step.Text, err)
		}
		if acc == "" {
			t.Errorf("step %q carries no acceptance marker, so completion would "+
				"be accepted on worker evidence alone -- it verifies nothing",
				strings.TrimSpace(step.Text))
		}
	})
	if steps == 0 {
		t.Fatal("no steps found; this test would assert nothing")
	}
}

// TestSelfTestPlanDependenciesResolve validates the import GRAPH, not just that
// the markdown parses.
//
// plan.Parse accepts an `after` naming a task that does not exist; the failure
// surfaces later, at import. Since this plan is only ever run by hand, that
// would present as tasks stuck pending forever -- indistinguishable from a
// product defect, which is the confusion the self-test exists to remove.
func TestSelfTestPlanDependenciesResolve(t *testing.T) {
	p := readSelfTestPlan(t)

	ids := map[string]bool{}
	walkSteps(p.Groups, func(step plan.Step) {
		if step.Metadata != nil && step.Metadata.ID != "" {
			ids[step.Metadata.ID] = true
		}
	})
	if len(ids) == 0 {
		t.Fatal("no task ids found; this test would assert nothing")
	}

	var edges int
	walkSteps(p.Groups, func(step plan.Step) {
		if step.Metadata == nil {
			return
		}
		deps, _ := step.Metadata.DependsOn()
		for _, dep := range deps {
			edges++
			if !ids[dep] {
				t.Errorf("step %q depends on %q, which no step declares; that task "+
					"would sit pending forever, reading as a product hang rather "+
					"than a plan typo", step.Metadata.ID, dep)
			}
		}
	})
	if edges == 0 {
		t.Fatal("no dependency edges found; the plan is meant to be a DAG and " +
			"this test would assert nothing")
	}
}
