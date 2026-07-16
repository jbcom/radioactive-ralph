package plan

import (
	"reflect"
	"testing"
)

func mustParse(t *testing.T, md string) *Plan {
	t.Helper()
	p, err := Parse([]byte(md))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return p
}

func TestDecomposeSequentialGroupsGateOnEachOther(t *testing.T) {
	p := mustParse(t, `# Do first

- alpha
- beta

# Do next

- gamma
`)

	// Nothing done: first group is parallel, both its steps are ready;
	// the second group must not appear yet.
	ready, parallel := Decompose(p, map[string]bool{})
	if !parallel {
		t.Fatalf("parallel = false, want true")
	}
	if len(ready) != 2 || ready[0].Text != "alpha" || ready[1].Text != "beta" {
		t.Fatalf("ready = %+v", ready)
	}

	// First group fully done: now the second group's step is ready. The
	// second group is still an unordered (parallel) list -- Parallel
	// reflects the list's type, not how many steps happen to be
	// pending -- so parallel stays true even with a single step.
	done := map[string]bool{"0.0": true, "0.1": true}
	ready, parallel = Decompose(p, done)
	if !parallel {
		t.Fatalf("parallel = false, want true (group's list is unordered)")
	}
	if len(ready) != 1 || ready[0].Text != "gamma" {
		t.Fatalf("ready = %+v", ready)
	}

	// Everything done: nothing left.
	done["1.0"] = true
	ready, parallel = Decompose(p, done)
	if ready != nil || parallel {
		t.Fatalf("ready = %+v, parallel = %v, want nil/false", ready, parallel)
	}
}

func TestDecomposeOrderedListReturnsOnlyFirstPending(t *testing.T) {
	p := mustParse(t, `# Group

1. first
2. second
3. third
`)
	ready, parallel := Decompose(p, map[string]bool{})
	if parallel {
		t.Fatalf("parallel = true, want false")
	}
	if len(ready) != 1 || ready[0].Text != "first" {
		t.Fatalf("ready = %+v, want [first]", ready)
	}

	ready, _ = Decompose(p, map[string]bool{"0.0": true})
	if len(ready) != 1 || ready[0].Text != "second" {
		t.Fatalf("ready = %+v, want [second]", ready)
	}

	ready, _ = Decompose(p, map[string]bool{"0.0": true, "0.1": true})
	if len(ready) != 1 || ready[0].Text != "third" {
		t.Fatalf("ready = %+v, want [third]", ready)
	}

	ready, parallel = Decompose(p, map[string]bool{"0.0": true, "0.1": true, "0.2": true})
	if ready != nil || parallel {
		t.Fatalf("ready = %+v, parallel = %v, want nil/false", ready, parallel)
	}
}

func TestDecomposeUnorderedListReturnsAllPendingInParallel(t *testing.T) {
	p := mustParse(t, `# Group

- a
- b
- c
`)
	ready, parallel := Decompose(p, map[string]bool{"0.1": true})
	if !parallel {
		t.Fatalf("parallel = false, want true")
	}
	if len(ready) != 2 || ready[0].Text != "a" || ready[1].Text != "c" {
		t.Fatalf("ready = %+v, want [a, c]", ready)
	}
}

func TestDecomposeRecursesIntoFirstIncompleteSubgroup(t *testing.T) {
	p := mustParse(t, `# Deploy

## Build

1. compile
2. package

## Push

- tag
- push
`)
	// Nothing done: recurse into Build (first subgroup), sequential,
	// first pending step only.
	ready, parallel := Decompose(p, map[string]bool{})
	if parallel {
		t.Fatalf("parallel = true, want false (Build is ordered)")
	}
	if len(ready) != 1 || ready[0].Text != "compile" {
		t.Fatalf("ready = %+v, want [compile]", ready)
	}

	// Build's first step done: still in Build, now "package".
	ready, _ = Decompose(p, map[string]bool{"0.0.0": true})
	if len(ready) != 1 || ready[0].Text != "package" {
		t.Fatalf("ready = %+v, want [package]", ready)
	}

	// Build fully done: recurse into Push (parallel), both steps ready.
	ready, parallel = Decompose(p, map[string]bool{"0.0.0": true, "0.0.1": true})
	if !parallel {
		t.Fatalf("parallel = false, want true (Push is unordered)")
	}
	if len(ready) != 2 || ready[0].Text != "tag" || ready[1].Text != "push" {
		t.Fatalf("ready = %+v, want [tag, push]", ready)
	}

	// Everything done: nothing left.
	done := map[string]bool{"0.0.0": true, "0.0.1": true, "0.1.0": true, "0.1.1": true}
	ready, parallel = Decompose(p, done)
	if ready != nil || parallel {
		t.Fatalf("ready = %+v, parallel = %v, want nil/false", ready, parallel)
	}
}

func TestStepRefIDStableAcrossReparse(t *testing.T) {
	md := `# Deploy

## Build

1. compile
2. package
`
	p1 := mustParse(t, md)
	p2 := mustParse(t, md)

	ids1 := p1.StepIDs()
	ids2 := p2.StepIDs()
	if !reflect.DeepEqual(ids1, ids2) {
		t.Fatalf("StepIDs differ across re-parses of identical source: %v vs %v", ids1, ids2)
	}
	want := []string{"0.0.0", "0.0.1"}
	if !reflect.DeepEqual(ids1, want) {
		t.Fatalf("StepIDs = %v, want %v", ids1, want)
	}
}

func TestDecomposeRefsMatchesDecompose(t *testing.T) {
	p := mustParse(t, `# Group

- a
- b
`)
	ready, parallel := Decompose(p, map[string]bool{})
	readyRefs, refs, parallelRefs := DecomposeRefs(p, map[string]bool{})

	if !reflect.DeepEqual(ready, readyRefs) {
		t.Fatalf("Decompose and DecomposeRefs steps differ: %+v vs %+v", ready, readyRefs)
	}
	if parallel != parallelRefs {
		t.Fatalf("parallel mismatch: %v vs %v", parallel, parallelRefs)
	}
	wantRefs := []StepRef{{GroupPath: []int{0}, Index: 0}, {GroupPath: []int{0}, Index: 1}}
	if !reflect.DeepEqual(refs, wantRefs) {
		t.Fatalf("refs = %+v, want %+v", refs, wantRefs)
	}
}

func TestStepAtResolvesRef(t *testing.T) {
	p := mustParse(t, `# Deploy

## Build

- compile
`)
	ref := StepRef{GroupPath: []int{0, 0}, Index: 0}
	step, group, err := p.StepAt(ref)
	if err != nil {
		t.Fatalf("StepAt: %v", err)
	}
	if step.Text != "compile" {
		t.Errorf("step.Text = %q", step.Text)
	}
	if group.Heading != "Build" {
		t.Errorf("group.Heading = %q", group.Heading)
	}
}

func TestStepAtOutOfRange(t *testing.T) {
	p := mustParse(t, `# Group

- a
`)
	if _, _, err := p.StepAt(StepRef{GroupPath: []int{5}, Index: 0}); err == nil {
		t.Fatal("expected error for out-of-range group index")
	}
	if _, _, err := p.StepAt(StepRef{GroupPath: []int{0}, Index: 5}); err == nil {
		t.Fatal("expected error for out-of-range step index")
	}
}

func TestDecomposeEmptyPlan(t *testing.T) {
	p := mustParse(t, "")
	ready, parallel := Decompose(p, map[string]bool{})
	if ready != nil || parallel {
		t.Fatalf("ready = %+v, parallel = %v, want nil/false for empty plan", ready, parallel)
	}
}
