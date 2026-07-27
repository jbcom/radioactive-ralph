package plan

import (
	"strings"
	"testing"
)

// TestParseStepWithoutMetadataLeavesNilMetadata pins the degenerate case: every
// plan written before this grammar existed must parse exactly as it did, with no
// metadata attached. This is the guarantee that makes a linear plan the
// degenerate DAG rather than a second code path.
func TestParseStepWithoutMetadataLeavesNilMetadata(t *testing.T) {
	parsed, err := Parse([]byte("# Group\n\n1. first\n2. second\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	steps := parsed.Groups[0].Steps
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(steps))
	}
	for i, s := range steps {
		if s.Metadata != nil {
			t.Errorf("step %d Metadata = %+v, want nil", i, s.Metadata)
		}
	}
}

// TestParseTaskMetadataAfterOmittedVersusEmpty is the load-bearing distinction.
// An OMITTED after keeps document order, so annotating a step with a team or
// binding cannot silently reorder execution. An EXPLICITLY EMPTY after makes the
// step a root. Collapsing the two would let one plan import as two graphs.
func TestParseTaskMetadataAfterOmittedVersusEmpty(t *testing.T) {
	md := "# Group\n\n" +
		"1. annotated but no ordering opinion\n\n" +
		"   ```ralph-task\n   {\"id\": \"a\", \"team\": \"alpha\"}\n   ```\n\n" +
		"2. explicit root\n\n" +
		"   ```ralph-task\n   {\"id\": \"b\", \"after\": []}\n   ```\n\n" +
		"3. explicit edge\n\n" +
		"   ```ralph-task\n   {\"id\": \"c\", \"after\": [\"a\"]}\n   ```\n"

	parsed, err := Parse([]byte(md))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	steps := parsed.Groups[0].Steps
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}

	// Case: after omitted -> not stated, so document order applies.
	ids, stated := steps[0].Metadata.DependsOn()
	if stated {
		t.Errorf("omitted after: stated = true, want false (document order must apply)")
	}
	if ids != nil {
		t.Errorf("omitted after: ids = %v, want nil", ids)
	}
	if steps[0].Metadata.Team != "alpha" {
		t.Errorf("Team = %q, want alpha", steps[0].Metadata.Team)
	}

	// Case: after [] -> stated with no edges, an explicit root.
	ids, stated = steps[1].Metadata.DependsOn()
	if !stated {
		t.Errorf("empty after: stated = false, want true (explicit root)")
	}
	if len(ids) != 0 {
		t.Errorf("empty after: ids = %v, want empty", ids)
	}

	// Case: after [ids] -> exactly those edges.
	ids, stated = steps[2].Metadata.DependsOn()
	if !stated || len(ids) != 1 || ids[0] != "a" {
		t.Errorf("explicit after: ids = %v stated = %v, want [a] true", ids, stated)
	}
}

func TestParseTaskMetadataRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			// A typo'd key must fail the import, not import with no edges and
			// silently run in the wrong order.
			name: "unknown field",
			body: `{"id": "a", "dependsOn": ["b"]}`,
			want: "unknown field",
		},
		{
			// null is indistinguishable from omitted after decoding, and the two
			// mean different things — so refuse rather than guess.
			name: "explicit null after",
			body: `{"id": "a", "after": null}`,
			want: "must not be null",
		},
		{
			name: "missing id",
			body: `{"after": []}`,
			want: "non-empty",
		},
		{
			name: "empty id",
			body: `{"id": "", "after": []}`,
			want: "non-empty",
		},
		{
			name: "trailing json value",
			body: `{"id": "a"} {"id": "b"}`,
			want: "more than one JSON value",
		},
		{
			// The duplicate-key scan reads the token stream first, so it is what
			// reports malformed input. Assert the input is refused rather than
			// pinning which layer refuses it.
			name: "malformed json",
			body: `{"id": "a",}`,
			want: "ralph-task",
		},
		{
			// encoding/json keeps the LAST value for a repeated key, so this
			// would otherwise decode as an unconditioned root and dispatch
			// before "prepare" — the exact ordering guarantee this grammar exists
			// to provide.
			name: "duplicate after key drops a dependency",
			body: `{"id": "x", "after": ["prepare"], "after": []}`,
			want: "repeats key",
		},
		{
			// A duplicate can also hide a null from rejectNullMetadataFields,
			// because the map decode that check relies on has already collapsed
			// the pair.
			name: "duplicate key hides a null",
			body: `{"id": "x", "after": null, "after": []}`,
			want: "repeats key",
		},
		{
			// Nested objects get their own key scope, so a repeat inside binding
			// must be caught too.
			name: "duplicate nested binding key",
			body: `{"id": "x", "binding": {"provider": "claude", "provider": "codex"}}`,
			want: "repeats key",
		},
		{
			// Objects inside arrays are scanned as well.
			name: "duplicate key in an array element",
			body: `{"id": "x", "inputs": [{"path": "a", "path": "b"}]}`,
			want: "repeats key",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			md := "# Group\n\n1. step\n\n   ```ralph-task\n   " + tc.body + "\n   ```\n"
			_, err := Parse([]byte(md))
			if err == nil {
				t.Fatalf("Parse accepted %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestParseTaskMetadataRejectsTwoBlocks refuses an ambiguous step rather than
// picking one block and ignoring the other.
func TestParseTaskMetadataRejectsTwoBlocks(t *testing.T) {
	md := "# Group\n\n1. step\n\n" +
		"   ```ralph-task\n   {\"id\": \"a\"}\n   ```\n\n" +
		"   ```ralph-task\n   {\"id\": \"b\"}\n   ```\n"
	if _, err := Parse([]byte(md)); err == nil {
		t.Fatal("Parse accepted two metadata blocks on one step")
	}
}

// TestParseTaskMetadataFullBinding covers the whole field surface so a rename or
// dropped json tag is caught here rather than by a plan that silently loses its
// provider pin.
func TestParseTaskMetadataFullBinding(t *testing.T) {
	md := "# Group\n\n1. step\n\n   ```ralph-task\n   " + `{
     "id": "full",
     "after": ["prev"],
     "team": "team/alpha",
     "binding": {
       "mode": "exact", "alias": "a1", "provider": "claude",
       "model": "opus", "effort": "high", "calibration": "c1",
       "repetitions": 3, "fixture": "f1"
     },
     "requires": ["fanout"],
     "providers": ["claude", "codex"],
     "differentFrom": ["other"],
     "inputs": [{"path": "in.txt", "sha256": "abc"}],
     "outputs": [{"path": "out.txt", "mode": "exclusive"}]
   }` + "\n   ```\n"

	parsed, err := Parse([]byte(md))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	m := parsed.Groups[0].Steps[0].Metadata
	if m == nil {
		t.Fatal("Metadata is nil")
	}
	if m.ID != "full" || m.Team != "team/alpha" {
		t.Errorf("ID/Team = %q/%q", m.ID, m.Team)
	}
	if m.Binding.Provider != "claude" || m.Binding.Model != "opus" || m.Binding.Repetitions != 3 {
		t.Errorf("Binding = %+v", m.Binding)
	}
	if len(m.Requires) != 1 || m.Requires[0] != "fanout" {
		t.Errorf("Requires = %v", m.Requires)
	}
	if len(m.Providers) != 2 || len(m.DifferentFrom) != 1 {
		t.Errorf("Providers = %v DifferentFrom = %v", m.Providers, m.DifferentFrom)
	}
	if len(m.Inputs) != 1 || m.Inputs[0].Path != "in.txt" || m.Inputs[0].SHA256 != "abc" {
		t.Errorf("Inputs = %+v", m.Inputs)
	}
	if len(m.Outputs) != 1 || m.Outputs[0].Path != "out.txt" || m.Outputs[0].Mode != "exclusive" {
		t.Errorf("Outputs = %+v", m.Outputs)
	}
}

// TestDependsOnHandlesNilReceiver keeps the unannotated path allocation-free and
// nil-safe: callers deriving edges hold a *TaskMetadata that is usually nil.
func TestDependsOnHandlesNilReceiver(t *testing.T) {
	var m *TaskMetadata
	if ids, stated := m.DependsOn(); stated || ids != nil {
		t.Fatalf("nil receiver: ids = %v stated = %v, want nil false", ids, stated)
	}
}
