package plan

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestDocumentedCapabilityExampleParses reads the `requires` example out of the
// operator guide and parses it.
//
// The example is EXTRACTED rather than restated: a copy in the test would let
// the guide drift into documenting a shape the parser rejects, and the guide is
// what an operator actually writes a plan from.
func TestDocumentedCapabilityExampleParses(t *testing.T) {
	guide := readGuide(t)

	const marker = "## Capability requirements"
	idx := strings.Index(guide, marker)
	if idx < 0 {
		t.Fatalf("guide has no %q section — the capability grammar is enforced in "+
			"dispatch and must stay documented", marker)
	}
	section := guide[idx:]
	if end := strings.Index(section[len(marker):], "\n## "); end >= 0 {
		section = section[:len(marker)+end]
	}

	example := extractMarkdownExample(t, section)
	parsed, err := Parse([]byte(example))
	if err != nil {
		t.Fatalf("the documented example does not parse: %v\n\n%s", err, example)
	}

	var found *TaskMetadata
	for _, group := range parsed.Groups {
		for _, step := range group.Steps {
			if step.Metadata != nil && len(step.Metadata.Requires) > 0 {
				found = step.Metadata
			}
		}
	}
	if found == nil {
		t.Fatal("the documented example produced no step with a requires list; " +
			"the fence indentation in the guide is what makes it attach to the step")
	}
	for _, key := range found.Requires {
		if !documentedCapabilityKeys(t, section)[key] {
			t.Errorf("example requires %q but the section's key table does not list it", key)
		}
	}
}

// TestDocumentedCapabilityKeysAreTheEnforcedOnes is the anti-drift check on the
// vocabulary itself: the table in the guide must name exactly the keys dispatch
// accepts, or an operator writes a plan against a key that blocks every step.
//
// The enforced set lives in internal/provider; this asserts against a literal
// list so a key added there without a doc update fails HERE, where the guide is.
func TestDocumentedCapabilityKeysAreTheEnforcedOnes(t *testing.T) {
	guide := readGuide(t)
	idx := strings.Index(guide, "## Capability requirements")
	if idx < 0 {
		t.Fatal("guide has no capability requirements section")
	}
	documented := documentedCapabilityKeys(t, guide[idx:])

	// Mirrors provider.KnownCapability. internal/plan must not import
	// internal/provider (the parser knows nothing about providers), so the list
	// is restated — and a mismatch surfaces as a failing test rather than a
	// plan that blocks in production.
	enforced := []string{"native_fanout", "resume", "append_system_prompt"}
	for _, key := range enforced {
		if !documented[key] {
			t.Errorf("capability %q is enforced by dispatch but not documented", key)
		}
	}
	for key := range documented {
		if !contains(enforced, key) {
			t.Errorf("guide documents capability %q that dispatch does not accept — "+
				"an operator using it would have every such step blocked", key)
		}
	}
}

func readGuide(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "guides", "plan-format.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

// extractMarkdownExample pulls the body of the section's ```markdown fence.
//
// The example itself contains a nested ```ralph-task fence, so the closing
// delimiter is found by scanning for a fence at column 0 rather than by taking
// the first "```" — which would truncate the example mid-step.
func extractMarkdownExample(t *testing.T, section string) string {
	t.Helper()
	const open = "```markdown\n"
	start := strings.Index(section, open)
	if start < 0 {
		t.Fatal("capability section has no ```markdown example")
	}
	lines := strings.Split(section[start+len(open):], "\n")
	for i, line := range lines {
		if line == "```" {
			return strings.Join(lines[:i], "\n") + "\n"
		}
	}
	t.Fatal("capability example fence is never closed at column 0")
	return ""
}

// documentedCapabilityKeys reads the key column of the section's table.
func documentedCapabilityKeys(t *testing.T, section string) map[string]bool {
	t.Helper()
	row := regexp.MustCompile("(?m)^\\| `([a-z_]+)` \\|")
	keys := map[string]bool{}
	for _, m := range row.FindAllStringSubmatch(section, -1) {
		keys[m[1]] = true
	}
	if len(keys) == 0 {
		t.Fatal("capability section has no key table")
	}
	return keys
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
