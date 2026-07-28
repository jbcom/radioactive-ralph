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
	bin, args, err := wrapCommand(p, "/bin/sh", []string{"-c", "printf x > " + inGrant})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
		t.Fatalf("write to the GRANTED path failed (%v): %s", err, out)
	}

	// Outside root AND outside the grant: must still be refused.
	bin, args, err = wrapCommand(p, "/bin/sh", []string{"-c", "printf x > " + forbidden})
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	_, _ = exec.Command(bin, args...).CombinedOutput()
	if _, err := os.Stat(forbidden); err == nil {
		t.Fatalf("a write to %q SUCCEEDED while only %q was granted; the allowance "+
			"re-opened the boundary generally instead of widening it by one subpath, "+
			"so every provider runs unconfined while the config claims otherwise",
			forbidden, granted)
	}
}
