package provider

import "testing"

// TestCodexIsContainableWithItsDeclaredPath replaces an earlier test asserting
// codex CANNOT be contained. That was true, and is no longer.
//
// It failed at startup under the write-deny profile, and I concluded from ONE
// bisection step (TMPDIR re-allowed, still failing) that no policy widening
// existed. Bisecting properly found it needs exactly $HOME/.codex, which it now
// declares -- so it is contained rather than permanently spared.
//
// The capability itself is NOT removed. It remains the honest answer for a
// provider whose requirement is unknown, and the refusal path it feeds is what
// prevents a silent downgrade. What changed is that codex no longer needs it.
func TestCodexIsContainableWithItsDeclaredPath(t *testing.T) {
	cfg := defaultCodexProvider()
	if !supportsContainment(cfg) {
		t.Fatal("codex declares it cannot be contained, but with ~/.codex granted a " +
			"real turn runs confined -- leaving the flag set means its turns run " +
			"unconfined on projects that asked for a boundary")
	}
	if len(cfg.WritePaths) == 0 {
		t.Fatal("codex declares no WritePaths; without ~/.codex it cannot start " +
			"under containment, so marking it capable would break every contained turn")
	}
}

// TestOpencodeIsContainableWithItsDeclaredPath is the same correction for
// opencode, which was found only because the live test covers EVERY detected
// provider rather than the first one.
func TestOpencodeIsContainableWithItsDeclaredPath(t *testing.T) {
	cfg := defaultOpencodeProvider()
	if !supportsContainment(cfg) {
		t.Fatal("opencode declares it cannot be contained, but with " +
			"~/.local/share/opencode granted a real turn runs confined")
	}
	if len(cfg.WritePaths) == 0 {
		t.Fatal("opencode declares no WritePaths")
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
