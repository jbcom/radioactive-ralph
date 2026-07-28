package plan

import "testing"

// docExamplePlan is the exact example from docs/guides/plan-format.md's
// "Task annotations" section. Documentation that lies about the format is
// worse than none, so the example is executed rather than trusted.
const docExamplePlan = "# Build and verify\n\n" +
	"1. compile the binary\n\n" +
	"   ```ralph-task\n   {\"id\": \"build\"}\n   ```\n\n" +
	"2. run the integration suite\n\n" +
	"   ```ralph-task\n   {\"id\": \"integration\", \"after\": [\"build\"]}\n   ```\n\n" +
	"3. run the linters\n\n" +
	"   ```ralph-task\n   {\"id\": \"lint\", \"after\": [\"build\"]}\n   ```\n"

// TestDocumentedExampleParses proves the guide's example is real: it parses,
// and the ids and edges are what the surrounding prose claims.
func TestDocumentedExampleParses(t *testing.T) {
	parsed, err := Parse([]byte(docExamplePlan))
	if err != nil {
		t.Fatalf("the documented example does not parse: %v", err)
	}
	steps := parsed.Groups[0].Steps
	if len(steps) != 3 {
		t.Fatalf("got %d steps, want 3", len(steps))
	}

	for i, want := range []struct {
		id    string
		after []string
	}{
		{"build", nil},
		{"integration", []string{"build"}},
		{"lint", []string{"build"}},
	} {
		md := steps[i].Metadata
		if md == nil {
			t.Fatalf("step %d has no metadata", i)
		}
		if md.ID != want.id {
			t.Errorf("step %d id = %q, want %q", i, md.ID, want.id)
		}
		ids, stated := md.DependsOn()
		if len(want.after) == 0 {
			if stated {
				t.Errorf("step %d states after; the guide shows it omitted", i)
			}
			continue
		}
		if !stated || len(ids) != len(want.after) || ids[0] != want.after[0] {
			t.Errorf("step %d after = %v (stated=%v), want %v", i, ids, stated, want.after)
		}
	}
}

// TestDocumentedAfterSemantics executes the three-row table in the guide, so
// the distinction it draws between omitted, empty, and populated `after`
// cannot drift from the parser.
func TestDocumentedAfterSemantics(t *testing.T) {
	md := "# G\n\n" +
		"1. omitted\n\n   ```ralph-task\n   {\"id\": \"a\"}\n   ```\n\n" +
		"2. explicit root\n\n   ```ralph-task\n   {\"id\": \"b\", \"after\": []}\n   ```\n\n" +
		"3. has edges\n\n   ```ralph-task\n   {\"id\": \"c\", \"after\": [\"a\"]}\n   ```\n"
	parsed, err := Parse([]byte(md))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	steps := parsed.Groups[0].Steps

	if _, stated := steps[0].Metadata.DependsOn(); stated {
		t.Error("omitted after reported as stated; document order must still apply")
	}
	ids, stated := steps[1].Metadata.DependsOn()
	if !stated || len(ids) != 0 {
		t.Errorf(`"after": [] = %v (stated=%v), want an explicit root`, ids, stated)
	}
	ids, stated = steps[2].Metadata.DependsOn()
	if !stated || len(ids) != 1 || ids[0] != "a" {
		t.Errorf(`"after": ["a"] = %v (stated=%v)`, ids, stated)
	}
}

// TestDocumentedFailuresAreRefused executes the guide's "fails closed" list.
// Each bullet claims a specific malformed block is REFUSED; a bullet that were
// wrong would be worse than silence, since a reader would trust the guarantee.
func TestDocumentedFailuresAreRefused(t *testing.T) {
	for name, body := range map[string]string{
		"unknown field":  `{"id": "a", "dependsOn": ["b"]}`,
		"null after":     `{"id": "a", "after": null}`,
		"duplicate key":  `{"id": "a", "after": ["prepare"], "after": []}`,
		"missing id":     `{"after": []}`,
		"trailing value": `{"id": "a"} {"id": "b"}`,
	} {
		t.Run(name, func(t *testing.T) {
			doc := "# G\n\n1. step\n\n   ```ralph-task\n   " + body + "\n   ```\n"
			if _, err := Parse([]byte(doc)); err == nil {
				t.Fatalf("the guide says this is refused, but it parsed: %s", body)
			}
		})
	}

	// Two blocks on one step.
	two := "# G\n\n1. step\n\n" +
		"   ```ralph-task\n   {\"id\": \"a\"}\n   ```\n\n" +
		"   ```ralph-task\n   {\"id\": \"b\"}\n   ```\n"
	if _, err := Parse([]byte(two)); err == nil {
		t.Fatal("the guide says two blocks on one step are refused, but it parsed")
	}
}

// TestAnnotatedStepRequiresAnExplicitID anchors the guide's `id` row.
//
// The positional-id default applies only to a step with NO ralph-task block.
// Inside a block the parser REJECTS a missing or empty id, so an operator
// reading "defaults to the step's positional id" and omitting it got a parse
// error instead of the documented fallback.
func TestAnnotatedStepRequiresAnExplicitID(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"omitted", `{"team": "alpha"}`},
		{"empty", `{"id": "", "team": "alpha"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			md := "# Group\n\n- step\n\n   ```ralph-task\n   " + tc.body + "\n   ```\n"
			if _, err := Parse([]byte(md)); err == nil {
				t.Fatal("a ralph-task block with no id parsed; the guide's id row " +
					"must not promise a positional default that does not apply here")
			}
		})
	}

	// The control: a step with NO block does get the positional default, which
	// is the case the guide's fallback describes.
	parsed, err := Parse([]byte("# Group\n\n- step\n"))
	if err != nil {
		t.Fatalf("unannotated step failed to parse: %v", err)
	}
	if len(parsed.Groups) == 0 || len(parsed.Groups[0].Steps) == 0 {
		t.Fatal("unannotated step produced no steps")
	}
}
