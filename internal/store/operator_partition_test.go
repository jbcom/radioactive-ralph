package store

import (
	"context"
	"strings"
	"testing"
)

// TestReadyPartitionShapeIsContentFree is the constraint that decides how this
// projection may be shaped at all, so it comes first.
//
// A partition's identity is (group path, declared binding key), and
// declaredBindingKey is NOT content-free: on the ordinary path it re-encodes
// the author's own binding fields, so a `provider` (or model, alias, fixture)
// string written in the plan appears in the key VERBATIM. Projecting BindingKey
// would therefore push author-written text across the observe boundary -- the
// boundary that deliberately withholds descriptions, acceptance commands, and
// artifacts.
//
// (The "unencodable:" fallback, which would embed the whole metadata document,
// is unreachable: undecodable metadata returns "" earlier, and marshalling a
// struct of strings and an int cannot fail. The leak this test guards is the
// NORMAL path, not that branch.)
//
// So the operator surface exposes partition SHAPE -- a stable opaque ordinal
// and a size -- and never the key itself.
func TestReadyPartitionShapeIsContentFree(t *testing.T) {
	secret := "s3cret-path/to/thing"
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "partition-content-free")
	planID := seedReadyGraph(t, s, projectID, "leaky", []GraphTaskSpec{
		bindingSpec("a", "0", secret),
		readySpec("b", "0"),
	})

	parts, err := s.ReadyPartitions(context.Background(), planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	if len(parts) == 0 {
		t.Fatal("no partitions; the leak this test guards would go unchecked")
	}
	// Prove the secret really is in the key, so a passing ordinal check below
	// means the projection dropped it rather than that it was never there.
	var keyed bool
	for _, p := range parts {
		if strings.Contains(p.BindingKey, secret) {
			keyed = true
		}
	}
	if !keyed {
		t.Fatal("author text did not reach BindingKey; this test would pass " +
			"vacuously -- re-check declaredBindingKey before trusting it")
	}
	for _, p := range parts {
		if ord := readyPartitionOrdinal(p); strings.Contains(ord, secret) {
			t.Fatalf("partition ordinal %q carries author-written text; the "+
				"observe boundary withholds author content", ord)
		}
	}
}

// TestReadyPartitionOrdinalIsStableAndDistinct pins the two properties an
// opaque ordinal must have to be useful at all: tasks in the SAME partition
// must share it (or the operator cannot see that one turn owns them), and
// tasks in DIFFERENT partitions must not (or fan-out looks like it merged
// work it actually kept separate).
func TestReadyPartitionOrdinalIsStableAndDistinct(t *testing.T) {
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "partition-ordinal")
	planID := seedReadyGraph(t, s, projectID, "ordinals", []GraphTaskSpec{
		bindingSpec("a", "0", "claude"),
		bindingSpec("b", "0", "claude"),
		bindingSpec("c", "0", "codex"),
	})

	parts, err := s.ReadyPartitions(context.Background(), planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("got %d partitions, want 2 (claude pair + codex singleton)", len(parts))
	}

	byOrdinal := map[string]int{}
	for _, p := range parts {
		byOrdinal[readyPartitionOrdinal(p)] += len(p.Tasks)
	}
	if len(byOrdinal) != 2 {
		t.Fatalf("2 distinct partitions collapsed to %d ordinal(s): %v\n"+
			"tasks a different provider will execute must not share an ordinal",
			len(byOrdinal), byOrdinal)
	}
}
