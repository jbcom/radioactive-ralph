//go:build darwin || linux

package contain

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestAllowanceDoesNotBecomeAnEscape is the guard that makes per-provider
// allowances safe to add at all.
//
// Granting one subpath must widen the boundary by exactly that subpath. If a
// grant accidentally re-opened writes generally -- a malformed profile line, a
// parameter that swallows the deny -- every provider would run effectively
// unconfined while the config still claimed a boundary, which is worse than no
// containment because it is trusted.
//
// Measures whether a write ESCAPES, not whether a profile line was emitted.
func TestAllowanceDoesNotBecomeAnEscape(t *testing.T) {
	if !available() {
		t.Skip("containment unavailable on this host")
	}
	root := t.TempDir()
	granted := t.TempDir()
	forbidden := filepath.Join(t.TempDir(), "escaped")

	p, err := NewPolicy(root, granted)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}

	// Inside the GRANTED path: must succeed, or the allowance does nothing.
	inGrant := filepath.Join(granted, "ok")
	if out, err := runWrapped(t, p, "printf x > "+inGrant); err != nil {
		t.Fatalf("write to the GRANTED path failed (%v): %s", err, out)
	}

	// Outside root AND outside the grant: must still be refused.
	_, _ = runWrapped(t, p, "printf x > "+forbidden)
	if _, err := os.Stat(forbidden); err == nil {
		t.Fatalf("a write to %q SUCCEEDED while only %q was granted; the allowance "+
			"re-opened the boundary generally instead of widening it by one subpath, "+
			"so every provider runs unconfined while the config claims otherwise",
			forbidden, granted)
	}
}

// runWrapped executes a shell snippet under the policy, routing through the
// stand-in helper binary on platforms whose Wrap re-execs os.Executable().
//
// On Linux, containment works by re-invoking the CURRENT binary with a helper
// flag; under `go test` that binary is the test binary, which does not parse
// the flag and exits "flag provided but not defined". The existing linux tests
// solve this with buildContainHelper, whose main() calls MaybeRunHelper the way
// the real CLI entry point does -- reused here rather than reinvented.
func runWrapped(t *testing.T, p Policy, script string) (string, error) {
	t.Helper()
	name, args, err := p.Wrap("/bin/sh", []string{"-c", script})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if selfExec, _ := os.Executable(); name == selfExec {
		name = buildContainHelper(t)
	}
	out, err := exec.Command(name, args...).CombinedOutput() //nolint:gosec // test-owned
	return string(out), err
}
