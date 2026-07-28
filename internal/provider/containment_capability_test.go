package provider

import "testing"

// TestCodexDeclaresItCannotBeContained pins the capability that keeps a
// containment-incompatible provider from failing opaquely.
//
// Verified against the installed codex 0.5x with real turns: it works
// uncontained ("CONFIRMED" in 12.6s) and dies in 0.5s under containment with
//
//	Error: failed to initialize in-process app-server client: Operation not permitted
//
// It is NOT a file-path problem, which rules out widening the policy. Under
// `(allow default)` with no write-deny at all codex succeeds; adding
// `(deny file-write*)` breaks it EVEN WITH TMPDIR re-allowed. So there is no
// narrow subpath to add -- the app-server needs a write the profile cannot
// enumerate.
//
// Without this flag the failure surfaces as a bare nonzero exit with a retry,
// which is the worst combination: nothing names containment as the cause and
// the retry cannot succeed. Same class as classifying a terminal failure as
// retryable (#278).
func TestCodexDeclaresItCannotBeContained(t *testing.T) {
	cfg := defaultCodexProvider()
	if cfg.SupportsContainment == nil {
		t.Fatal("codex leaves SupportsContainment unset, so it reads as capable; " +
			"a real codex turn cannot start under containment and must say so")
	}
	if *cfg.SupportsContainment {
		t.Fatal("codex declares SupportsContainment=true, but a real contained turn " +
			"dies at startup with \"failed to initialize in-process app-server client\"")
	}
}

// TestClaudeStillSupportsContainment is the other half. Without it the test
// above is satisfied by declaring every provider incapable, which would disable
// containment fleet-wide -- trading an opaque failure for a silent one.
//
// A real contained claude turn completes end to end: status=done, exit_code=0,
// with ordinary assistant output.
func TestClaudeStillSupportsContainment(t *testing.T) {
	cfg := defaultClaudeProvider()
	if cfg.SupportsContainment != nil && !*cfg.SupportsContainment {
		t.Fatal("claude declares it cannot be contained, but a real contained claude " +
			"turn reaches status=done; marking it incapable would silently drop a " +
			"working security boundary")
	}
}

// TestContainmentCapabilityDefaultsToCapable keeps the flag from becoming a
// silent opt-out for every provider that never sets it.
//
// Absent means CAPABLE, deliberately, and the direction matters. An unset flag
// meaning "cannot be contained" would let a new provider quietly skip the
// boundary; unset meaning "capable" makes the failure loud instead -- the turn
// breaks visibly and someone sets the flag with evidence, which is how codex
// got its value.
func TestContainmentCapabilityDefaultsToCapable(t *testing.T) {
	if !supportsContainment(BindingConfig{}) {
		t.Fatal("a BindingConfig that never mentions containment reads as INCAPABLE; " +
			"that turns an omission into a silently skipped security boundary")
	}
}

// TestOpencodeDeclaresItCannotBeContained covers the provider I did NOT find
// first, and only found by making the live contained test cover every detected
// provider instead of suggested[0].
//
// That omission is the whole reason this capability exists: I filed the
// containment blocker from the codex reproduction alone, which was the same
// narrow-evidence mistake one level down.
func TestOpencodeDeclaresItCannotBeContained(t *testing.T) {
	cfg := defaultOpencodeProvider()
	if cfg.SupportsContainment == nil || *cfg.SupportsContainment {
		t.Fatal("opencode does not declare SupportsContainment=false, but a real " +
			"contained opencode turn fails (status=pending, retry_count=1)")
	}
}
