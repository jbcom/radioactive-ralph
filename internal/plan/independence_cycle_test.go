package plan

import (
	"strings"
	"testing"
)

// mutualDifferentFromPlan has two tasks each naming the other.
const mutualDifferentFromPlan = "# Mutual cross-check\n\n" +
	"- write A\n\n" +
	"   ```ralph-task\n   {\"id\": \"a\", \"differentFrom\": [\"b\"]}\n   ```\n\n" +
	"- write B\n\n" +
	"   ```ralph-task\n   {\"id\": \"b\", \"differentFrom\": [\"a\"]}\n   ```\n"

// threeCycleDifferentFromPlan is the same trap one hop longer.
const threeCycleDifferentFromPlan = "# Ring cross-check\n\n" +
	"- one\n\n" +
	"   ```ralph-task\n   {\"id\": \"a\", \"differentFrom\": [\"b\"]}\n   ```\n\n" +
	"- two\n\n" +
	"   ```ralph-task\n   {\"id\": \"b\", \"differentFrom\": [\"c\"]}\n   ```\n\n" +
	"- three\n\n" +
	"   ```ralph-task\n   {\"id\": \"c\", \"differentFrom\": [\"a\"]}\n   ```\n"

// TestMutualDifferentFromIsRejectedAtImport pins that a cycle cannot import.
//
// Enforcement resolves a peer's domain from what it ACTUALLY RAN ON, so a task
// cannot be admitted until every peer it names has already run. Two tasks naming
// each other therefore deadlock by construction: admitting either requires the
// other to have run first, and neither ever can.
//
// The runtime symptom is the bad kind. It is not a fail-closed `blocked_*`
// state an operator can see and clear -- it is `worker.admission_refused` on
// every tick, forever, which looks exactly like a plan that is merely waiting.
// Nothing ever changes, and nothing says so.
//
// This is refused at import for the same reason a self-reference already is:
// the author is present, the plan is statically knowable to be unsatisfiable,
// and a constraint that can never be satisfied is not protection.
func TestMutualDifferentFromIsRejectedAtImport(t *testing.T) {
	for name, md := range map[string]string{
		"two-cycle":   mutualDifferentFromPlan,
		"three-cycle": threeCycleDifferentFromPlan,
	} {
		t.Run(name, func(t *testing.T) {
			// ValidateForImport, not Parse: import is the ingress contract where
			// the author is present, and Parse stays deliberately lenient so
			// already-stored historical plans remain inspectable.
			err := ValidateForImport([]byte(md))
			if err == nil {
				t.Fatal("a differentFrom cycle imported CLEAN; every task in it needs " +
					"another member of the cycle to have run first, so none is ever " +
					"admitted -- the plan retries forever with no terminal signal and " +
					"no operator-visible reason")
			}
			if !strings.Contains(err.Error(), "differentFrom") {
				t.Fatalf("import failed, but not for the cycle: %v", err)
			}
		})
	}
}

// TestAcyclicDifferentFromStillImports keeps the cycle check from becoming a ban
// on the feature. The ordinary shape -- a reviewer that must differ from the
// author -- is exactly what differentFrom is for and must still import.
func TestAcyclicDifferentFromStillImports(t *testing.T) {
	const md = "# Cross-check\n\n" +
		"- produce\n\n" +
		"   ```ralph-task\n   {\"id\": \"produce\"}\n   ```\n\n" +
		"- review\n\n" +
		"   ```ralph-task\n   {\"id\": \"review\", \"differentFrom\": [\"produce\"]}\n   ```\n"

	if err := ValidateForImport([]byte(md)); err != nil {
		t.Fatalf("a plain acyclic differentFrom failed to import: %v -- the cycle "+
			"check must reject unsatisfiable plans, not the feature's normal use", err)
	}
}
