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

// TestOperatorTasksAgreeWithReadyPartitions is the projection's real contract:
// the ordinal an operator sees must match the grouping dispatch actually uses.
//
// Computing the grouping a second way in the snapshot query would be a second
// DEFINITION of a partition, and the two would diverge the first time either
// changed -- leaving the operator confidently reading a grouping that dispatch
// no longer performs. So this asserts agreement with ReadyPartitions rather
// than asserting some particular hash value.
func TestOperatorTasksAgreeWithReadyPartitions(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "partition-projection")
	planID := seedReadyGraph(t, s, projectID, "waves", []GraphTaskSpec{
		bindingSpec("a", "0", "claude"),
		bindingSpec("b", "0", "claude"),
		bindingSpec("c", "0", "codex"),
		readySpec("d", "1"),
	})

	parts, err := s.ReadyPartitions(ctx, planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	want := map[string]string{} // task id -> ordinal dispatch would use
	for _, p := range parts {
		for _, task := range p.Tasks {
			want[task.ID] = readyPartitionOrdinal(p)
		}
	}

	items, err := operatorTasksForTest(ctx, s, projectID)
	if err != nil {
		t.Fatalf("operator tasks: %v", err)
	}
	if len(items) != len(want) {
		t.Fatalf("snapshot has %d task(s), partitions cover %d", len(items), len(want))
	}
	for _, item := range items {
		if got := item.PartitionOrdinal; got != want[item.ID] {
			t.Errorf("task %s: snapshot ordinal %q != dispatch ordinal %q; the "+
				"operator would read a grouping dispatch does not perform",
				item.ID, got, want[item.ID])
		}
	}

	// a and b share a partition; c and d must each differ from it and each other.
	if want["a"] != want["b"] {
		t.Error("a and b share a group AND a binding but got different ordinals")
	}
	if want["a"] == want["c"] || want["a"] == want["d"] || want["c"] == want["d"] {
		t.Errorf("distinct partitions collapsed: a=%q c=%q d=%q",
			want["a"], want["c"], want["d"])
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

// TestOperatorTasksDoNotPartitionUnreadyTasksWithTheirDependency is the case
// DOGFOODING found that TestOperatorTasksAgreeWithReadyPartitions could not:
// its fixture had no dependency edges, so every task was ready and agreement
// held vacuously.
//
// Running Ralph on its own plan showed `build` sharing a partition marker with
// the three tasks that declare after:[build]. A partition is "tasks ONE worker
// may own in ONE turn", and a task can never share a turn with its own
// dependency -- ReadyPartitions excludes the dependents until build completes,
// so the snapshot was claiming a grouping dispatch would never perform.
func TestOperatorTasksDoNotPartitionUnreadyTasksWithTheirDependency(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	projectID := mustCreateProject(t, s, "partition-unready")
	planID := seedReadyGraph(t, s, projectID, "deps", []GraphTaskSpec{
		readySpec("build", "0"),
		readySpec("race", "0", "build"),
		readySpec("e2e", "0", "build"),
	})

	// Only build is ready, so dispatch would coalesce nothing else with it.
	parts, err := s.ReadyPartitions(ctx, planID)
	if err != nil {
		t.Fatalf("ReadyPartitions: %v", err)
	}
	ready := map[string]bool{}
	for _, p := range parts {
		for _, task := range p.Tasks {
			ready[task.ID] = true
		}
	}
	if !ready["build"] || ready["race"] || ready["e2e"] {
		t.Fatalf("fixture wrong: ReadyPartitions returned %v, want build only "+
			"-- this test proves nothing if the dependents are already ready", ready)
	}

	items, err := operatorTasksForTest(ctx, s, projectID)
	if err != nil {
		t.Fatalf("operator tasks: %v", err)
	}
	byID := map[string]string{}
	for _, item := range items {
		byID[item.ID] = item.PartitionOrdinal
	}
	if byID["build"] != "" && byID["build"] == byID["race"] {
		t.Errorf("build and race share partition ordinal %q, but race declares "+
			"after:[build] and cannot run in the same turn; the marker claims a "+
			"grouping dispatch would never perform", byID["build"])
	}
	if byID["race"] != "" && byID["race"] == byID["e2e"] {
		t.Errorf("race and e2e share ordinal %q while both are UNREADY; a "+
			"partition is a dispatchable unit, and neither is dispatchable yet",
			byID["race"])
	}
}
