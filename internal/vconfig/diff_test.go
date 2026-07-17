package vconfig

import "testing"

// TestDiffConflictsFindsOverridingStanzaKey verifies DiffConflicts detects
// a projects: stanza key that would override an already-stored value, and
// ignores keys that are new (not present in stored) or identical.
func TestDiffConflictsFindsOverridingStanzaKey(t *testing.T) {
	stored := ProjectConfig{Values: map[string]any{
		"model":    "stored-model",
		"provider": "claude",
	}}
	incoming := map[string]any{
		"model":    "incoming-model", // conflicts: differs from stored
		"provider": "claude",         // identical: not a conflict
		"new_key":  "new-value",      // not in stored: not a conflict
	}

	conflicts := DiffConflicts(stored, incoming)
	if len(conflicts) != 1 {
		t.Fatalf("DiffConflicts returned %d conflicts, want 1: %+v", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c.Key != "model" || c.Stored != "stored-model" || c.Incoming != "incoming-model" {
		t.Errorf("conflict = %+v, want {model stored-model incoming-model}", c)
	}
}

// TestAutoRemoveStripsConflictingKeys verifies AutoRemove deletes the
// conflicting keys from incoming while leaving non-conflicting keys intact,
// and does not mutate the input map.
func TestAutoRemoveStripsConflictingKeys(t *testing.T) {
	incoming := map[string]any{
		"model":    "incoming-model",
		"provider": "claude",
		"new_key":  "new-value",
	}
	conflicts := []Conflict{{Key: "model", Stored: "stored-model", Incoming: "incoming-model"}}

	cleaned := AutoRemove(incoming, conflicts)

	if _, ok := cleaned["model"]; ok {
		t.Error("cleaned still contains conflicting key \"model\"")
	}
	if cleaned["provider"] != "claude" || cleaned["new_key"] != "new-value" {
		t.Errorf("cleaned = %+v, want provider/new_key preserved", cleaned)
	}
	// Original map must be untouched.
	if _, ok := incoming["model"]; !ok {
		t.Error("AutoRemove mutated the input map; want a copy")
	}
}

// TestDiffConflictsEmptyWhenNoOverlap verifies no conflicts are reported
// when stored and incoming share no keys at all.
func TestDiffConflictsEmptyWhenNoOverlap(t *testing.T) {
	stored := ProjectConfig{Values: map[string]any{"a": 1}}
	incoming := map[string]any{"b": 2}

	conflicts := DiffConflicts(stored, incoming)
	if len(conflicts) != 0 {
		t.Errorf("DiffConflicts = %+v, want empty", conflicts)
	}
}
