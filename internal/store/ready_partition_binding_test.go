package store

import (
	"context"
	"testing"
)

// bindingSpec is readySpec with a declared per-task binding in its metadata,
// the shape `{"binding":{"provider":"codex"}}` produces at import.
func bindingSpec(id, group, provider string) GraphTaskSpec {
	spec := readySpec(id, group)
	if provider != "" {
		spec.MetadataJSON = `{"id":"` + id + `","binding":{"provider":"` + provider + `"}}`
	}
	return spec
}

// TestReadyPartitionsSplitOnDeclaredBinding pins the half of partitioning that
// was specified and never built.
//
// A partition is "tasks one worker may own", and native fan-out hands the whole
// partition to ONE provider in ONE turn. Group path alone does not establish
// that: two tasks in the SAME leaf group can pin different providers, and
// merging them means one of those pins is silently discarded. The design spec
// is explicit that a partition requires "same leaf group, same resolved
// binding, same independence domain" and calls the alternative "silently wrong
// dispatch, not just suboptimal scheduling".
//
// Only the group half shipped. This is the same shape as the `providers` hole
// native fan-out had until #272: a per-task restriction that a coalesced turn
// cannot honour, and that nothing excluded from coalescing.
func TestReadyPartitionsSplitOnDeclaredBinding(t *testing.T) {
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "ready-partition-binding")
	planID := seedReadyGraph(t, s, projectID, "binding-wave", []GraphTaskSpec{
		bindingSpec("a", "0", "claude"),
		bindingSpec("b", "0", "codex"),
	})

	parts, err := s.ReadyPartitions(context.Background(), planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("got %d partition(s) for two tasks pinning DIFFERENT providers, want 2: %+v\n"+
			"a partition is delegated to one provider in one turn, so merging these "+
			"discards one task's declared binding -- the pin imports clean and does nothing",
			len(parts), parts)
	}
}

// TestReadyPartitionsKeepMatchingBindingsTogether is the other half. Without it
// the test above is satisfied by splitting every task into its own partition,
// which would disable fan-out entirely and trade a correctness hole for a
// silent performance regression.
func TestReadyPartitionsKeepMatchingBindingsTogether(t *testing.T) {
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "ready-partition-same-binding")
	planID := seedReadyGraph(t, s, projectID, "same-wave", []GraphTaskSpec{
		bindingSpec("a", "0", "claude"),
		bindingSpec("b", "0", "claude"),
	})

	parts, err := s.ReadyPartitions(context.Background(), planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	if len(parts) != 1 || len(parts[0].Tasks) != 2 {
		t.Fatalf("got %+v, want ONE partition holding both tasks: they share a leaf "+
			"group AND a declared binding, so one worker may own both; splitting them "+
			"costs fan-out for nothing", parts)
	}
}

// TestReadyPartitionsTreatUnpinnedAsItsOwnKey keeps an unpinned task from being
// merged into a pinned partition.
//
// An absent binding does not mean "compatible with whatever the pinned task
// wants" -- it means the pool resolves it, which may be a different provider.
// Merging the two would let a pinned task's turn silently execute an unpinned
// one, and that is the same absence-of-evidence reasoning differentFrom
// deliberately refuses.
func TestReadyPartitionsTreatUnpinnedAsItsOwnKey(t *testing.T) {
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "ready-partition-unpinned")
	planID := seedReadyGraph(t, s, projectID, "mixed-wave", []GraphTaskSpec{
		bindingSpec("a", "0", "claude"),
		bindingSpec("b", "0", ""), // no binding declared
	})

	parts, err := s.ReadyPartitions(context.Background(), planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("got %d partition(s), want 2: an unpinned task is not known to be "+
			"compatible with a pinned one, and merging them lets the pin decide a turn "+
			"it was never declared for: %+v", len(parts), parts)
	}
}

// TestDeclaredBindingKeyTreatsEmptyObjectAsUnpinned pins the degenerate shape.
//
// `"binding":{}` is present but pins nothing, so it must key identically to an
// absent binding. Keying it separately would split a partition -- costing
// fan-out -- on the strength of a declaration that restricts nothing.
func TestDeclaredBindingKeyTreatsEmptyObjectAsUnpinned(t *testing.T) {
	for name, md := range map[string]string{
		"absent":       `{"id":"a"}`,
		"empty object": `{"id":"a","binding":{}}`,
		"empty string": ``,
		"undecodable":  `{not json`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := declaredBindingKey(md); got != "" {
				t.Fatalf("declaredBindingKey(%q) = %q, want \"\": this declares no "+
					"restriction, so it must group with other unpinned tasks rather "+
					"than splitting a partition for nothing", md, got)
			}
		})
	}
}

// TestDeclaredBindingKeyIgnoresUnrelatedFields is what makes canonicalization
// necessary rather than comparing raw metadata_json: two tasks sharing a
// binding differ in id, description, and dependency fields, and keying on the
// whole document would split them apart.
func TestDeclaredBindingKeyIgnoresUnrelatedFields(t *testing.T) {
	a := declaredBindingKey(`{"id":"a","after":["x"],"binding":{"provider":"codex"}}`)
	b := declaredBindingKey(`{"id":"b","description":"other","binding":{"provider":"codex"}}`)
	if a == "" {
		t.Fatal("declaredBindingKey returned \"\" for a task that DOES pin a provider")
	}
	if a != b {
		t.Fatalf("keys differ (%q vs %q) for two tasks pinning the SAME provider; "+
			"partitioning on the raw document would split a group that one worker "+
			"may legitimately own", a, b)
	}
}

// TestDeclaredBindingKeyIsCollisionFree pins that two DIFFERENT bindings can
// never produce the same key.
//
// The first implementation joined fields with "|", which is ambiguous the moment
// a field can contain that byte -- and nothing forbids it. provider "claude|x"
// with model "y" and provider "claude" with model "x|y" produced BYTE-IDENTICAL
// keys, so two differently-pinned tasks merged into one partition and could be
// handed to a single provider turn: exactly the restriction this key exists to
// preserve, defeated by its own encoding.
//
// JSON escapes its own delimiters, so the encoding is injective.
func TestDeclaredBindingKeyIsCollisionFree(t *testing.T) {
	cases := map[string][2]string{
		"delimiter in provider vs model": {
			`{"binding":{"provider":"claude|x","model":"y"}}`,
			`{"binding":{"provider":"claude","model":"x|y"}}`,
		},
		"delimiter shifts one field over": {
			`{"binding":{"alias":"a|b","provider":"c"}}`,
			`{"binding":{"alias":"a","provider":"b|c"}}`,
		},
		"quote in a field": {
			`{"binding":{"provider":"a\"b","model":"c"}}`,
			`{"binding":{"provider":"a","model":"b\"c"}}`,
		},
	}
	for name, pair := range cases {
		t.Run(name, func(t *testing.T) {
			a, b := declaredBindingKey(pair[0]), declaredBindingKey(pair[1])
			if a == "" || b == "" {
				t.Fatalf("a declared binding keyed as unpinned: a=%q b=%q", a, b)
			}
			if a == b {
				t.Fatalf("COLLISION: %q and %q both key as %q; two differently-pinned "+
					"tasks would merge into one partition and be handed to a single "+
					"provider turn", pair[0], pair[1], a)
			}
		})
	}
}

// TestDeclaredBindingKeyIsStable pins that the same binding keys identically
// across calls and across field ORDER in the source document -- partitioning
// compares these keys, so an unstable encoding would split a group that one
// worker may legitimately own.
func TestDeclaredBindingKeyIsStable(t *testing.T) {
	a := declaredBindingKey(`{"binding":{"provider":"codex","model":"opus"}}`)
	b := declaredBindingKey(`{"binding":{"model":"opus","provider":"codex"}}`)
	if a == "" {
		t.Fatal("declaredBindingKey returned \"\" for a task that DOES pin a provider")
	}
	if a != b {
		t.Fatalf("key depends on source field order (%q vs %q); the same binding must "+
			"always produce the same key", a, b)
	}
}
