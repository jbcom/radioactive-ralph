package plan

import (
	"strings"
	"testing"
)

// differentFromPlan builds a two-step plan whose second step declares
// differentFrom.
func differentFromPlan(target string) []byte {
	return []byte("# Cross-check\n\n" +
		"- produce the migration\n\n" +
		"   ```ralph-task\n   {\"id\": \"produce\"}\n   ```\n\n" +
		"- review it\n\n" +
		"   ```ralph-task\n   {\"id\": \"review\", \"differentFrom\": [\"" + target + "\"]}\n   ```\n")
}

// TestDifferentFromMustNameAKnownTask rejects a reference no task satisfies.
//
// differentFrom is an independence constraint: run this task on a provider
// different from the one that produced the artifact it reviews. A reference to a
// task that does not exist cannot be satisfied OR violated, so it is silently
// vacuous — the review appears independent while nothing enforces it. That is
// worse than an unenforced field, because the plan LOOKS like it has a guarantee.
//
// Caught at import, where the author is present to fix it, rather than at
// dispatch where it would surface as a task that mysteriously never blocks.
func TestDifferentFromMustNameAKnownTask(t *testing.T) {
	err := ValidateForImport(differentFromPlan("does-not-exist"))
	if err == nil {
		t.Fatal("a differentFrom reference to an unknown task was accepted; the " +
			"constraint is vacuous, so the plan claims an independence guarantee " +
			"nothing enforces")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("err = %v, want it to name the unresolvable reference so the author "+
			"can find it", err)
	}
}

// TestDifferentFromCannotReferenceItself rejects self-reference.
//
// A task cannot run on a provider different from its own, so the constraint is
// unsatisfiable rather than merely vacuous — dispatch could only ever block it.
// Failing at import turns a permanent stall into an authoring error.
func TestDifferentFromCannotReferenceItself(t *testing.T) {
	err := ValidateForImport(differentFromPlan("review"))
	if err == nil {
		t.Fatal("a task declared differentFrom itself; that is unsatisfiable, so " +
			"dispatch could only ever block it forever")
	}
	if !strings.Contains(err.Error(), "review") {
		t.Errorf("err = %v, want it to name the self-referencing task", err)
	}
}

// TestDifferentFromAcceptsAKnownPeer is the control. Validation that rejected
// legitimate constraints would push authors to drop the field entirely.
func TestDifferentFromAcceptsAKnownPeer(t *testing.T) {
	if err := ValidateForImport(differentFromPlan("produce")); err != nil {
		t.Fatalf("a valid differentFrom reference was rejected: %v", err)
	}
}

// TestPlansWithoutDifferentFromAreUnaffected is the compatibility guard: every
// existing plan omits the field and none may start failing import.
func TestPlansWithoutDifferentFromAreUnaffected(t *testing.T) {
	md := []byte("# Group\n\n- step one\n- step two\n")
	if err := ValidateForImport(md); err != nil {
		t.Fatalf("an unannotated plan failed import: %v", err)
	}
}

// TestDifferentFromRejectsAnEmptyReference closes the inconsistency a review
// caught: unknown references were rejected while EMPTY ones were skipped.
//
// An empty string names no task, so it is vacuous for exactly the reason an
// unknown reference is — the plan reads as carrying an independence guarantee
// that nothing can enforce. Skipping it also means the author gets no signal
// that a list entry did nothing, which is how a mistyped constraint survives
// review.
func TestDifferentFromRejectsAnEmptyReference(t *testing.T) {
	for _, entry := range []string{"", "   ", "\t"} {
		md := []byte("# Cross-check\n\n" +
			"- produce\n\n" +
			"   ```ralph-task\n   {\"id\": \"produce\"}\n   ```\n\n" +
			"- review\n\n" +
			"   ```ralph-task\n   {\"id\": \"review\", \"differentFrom\": [\"" + entry + "\"]}\n   ```\n")
		if err := ValidateForImport(md); err == nil {
			t.Errorf("differentFrom [%q] was accepted; an entry naming no task is "+
				"vacuous for the same reason an unknown reference is, and skipping it "+
				"gives the author no signal that the entry did nothing", entry)
		}
	}
}
