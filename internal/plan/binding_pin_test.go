package plan

import (
	"strings"
	"testing"
)

// TestContradictoryBindingPinIsRejected pins that a plan cannot state two
// incompatible provider requirements at once.
//
// AllowedProviders resolves a declared binding.provider by returning it ALONE,
// which is correct only because import guarantees the two never disagree. Without
// that guarantee, providers:["codex"] + binding.provider:"claude" would silently
// honour the pin and run the task on a provider the plan explicitly excluded.
func TestContradictoryBindingPinIsRejected(t *testing.T) {
	const md = "# Conflict\n\n- work\n\n   ```ralph-task\n" +
		"   {\"id\":\"a\",\"providers\":[\"codex\"],\"binding\":{\"provider\":\"claude\"}}\n   ```\n"

	err := ValidateForImport([]byte(md))
	if err == nil {
		t.Fatal("a task pinning binding.provider=claude while restricting providers to " +
			"[codex] imported CLEAN; the two cannot both be honoured, so whichever " +
			"dispatch picks silently violates the other")
	}
	if !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("import failed, but not for the contradiction: %v", err)
	}
}

// TestAgreeingBindingPinImports keeps the check from banning the ordinary case:
// stating the same provider twice is redundant, not contradictory.
func TestAgreeingBindingPinImports(t *testing.T) {
	const md = "# Agree\n\n- work\n\n   ```ralph-task\n" +
		"   {\"id\":\"a\",\"providers\":[\"codex\"],\"binding\":{\"provider\":\"codex\"}}\n   ```\n"
	if err := ValidateForImport([]byte(md)); err != nil {
		t.Fatalf("a task whose pin AGREES with its providers list was rejected: %v", err)
	}
}

// TestBindingPinAloneImports covers the shape this whole change exists for: a
// pin with no providers list is the normal way to express it.
func TestBindingPinAloneImports(t *testing.T) {
	const md = "# Pin\n\n- work\n\n   ```ralph-task\n" +
		"   {\"id\":\"a\",\"binding\":{\"provider\":\"codex\"}}\n   ```\n"
	if err := ValidateForImport([]byte(md)); err != nil {
		t.Fatalf("a task declaring only binding.provider was rejected: %v", err)
	}
}

// TestBindingPinDrivesAllowedProviders is the wiring assertion: the pin must
// reach the SAME accessor `providers` uses, or every consumer of that accessor
// silently ignores it.
func TestBindingPinDrivesAllowedProviders(t *testing.T) {
	m := &TaskMetadata{}
	m.Binding.Provider = "codex"
	got := m.AllowedProviders()
	if len(got) != 1 || got[0] != "codex" {
		t.Fatalf("AllowedProviders() = %v, want [codex]: a declared pin that does not "+
			"reach this accessor is invisible to dispatch admission, the independence "+
			"rotation, and fan-out coalescing alike", got)
	}
}
