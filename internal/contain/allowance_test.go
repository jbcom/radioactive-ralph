//go:build darwin || linux

package contain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPolicyGrantsDeclaredExtraPaths is the fix for the wrong conclusion in
// #285, where I decided no policy widening was available after bisecting one
// step.
//
// Measured: codex needs $HOME/.codex and opencode needs
// $HOME/.local/share/opencode. Neither needs a blanket $HOME grant, which also
// works and would gut the boundary — the same mistake that got TMPDIR removed
// from the allow-set, since on macOS it resolves under /private/tmp and
// re-opened the boundary wholesale.
func TestPolicyGrantsDeclaredExtraPaths(t *testing.T) {
	root := t.TempDir()
	extra := t.TempDir()

	p, err := NewPolicy(root, extra)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	if len(p.ExtraWritable) != 1 {
		t.Fatalf("ExtraWritable = %v, want one entry", p.ExtraWritable)
	}
	resolvedExtra, _ := filepath.EvalSymlinks(extra)
	if p.ExtraWritable[0] != resolvedExtra {
		t.Fatalf("ExtraWritable[0] = %q, want the SYMLINK-RESOLVED %q; a policy "+
			"written against a link names the link rather than its target, so a write "+
			"through the resolved path lands outside a boundary that appears to "+
			"contain it", p.ExtraWritable[0], resolvedExtra)
	}
}

// TestPolicyRejectsRelativeExtraPath keeps an allowance from being ambiguous.
// A relative path resolves against whatever cwd the provider happens to have,
// which is not a boundary anyone reasoned about.
func TestPolicyRejectsRelativeExtraPath(t *testing.T) {
	if _, err := NewPolicy(t.TempDir(), "relative/path"); err == nil {
		t.Fatal("a RELATIVE extra path was accepted; it resolves against the " +
			"provider's cwd, so the granted boundary depends on where the turn runs")
	}
}

// TestPolicyRejectsAnExtraPathThatSwallowsTheBoundary is the guard that keeps
// this feature from becoming an escape hatch.
//
// Granting "/" or $HOME wholesale satisfies every provider and destroys the
// point of containment. The allowance exists for a CLI's own state directory,
// not as a way to opt out while still reading as contained.
func TestPolicyRejectsAnExtraPathThatSwallowsTheBoundary(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	for _, bad := range []string{"/", home} {
		if _, err := NewPolicy(t.TempDir(), bad); err == nil {
			t.Errorf("extra path %q was accepted; granting it makes containment "+
				"vacuous while the config still claims a boundary", bad)
		}
	}
}

// TestWrapPassesExtraPathsAsParameters pins that an allowance cannot change the
// POLICY's meaning, only widen it by one named subpath.
//
// Interpolating a path into the profile text lets a path containing quotes or
// parens rewrite the profile — the reason the root is already passed with -D.
func TestWrapPassesExtraPathsAsParameters(t *testing.T) {
	root := t.TempDir()
	extra := t.TempDir()
	p, err := NewPolicy(root, extra)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	_, args, err := wrapCommand(p, "true", nil)
	if err != nil {
		if err == ErrContainmentUnavailable {
			t.Skip("containment unavailable on this host")
		}
		t.Fatalf("wrapCommand: %v", err)
	}
	joined := strings.Join(args, " ")
	resolvedExtra, _ := filepath.EvalSymlinks(extra)
	if !strings.Contains(joined, resolvedExtra) {
		t.Fatalf("wrapped args do not mention the extra path %q: %v", resolvedExtra, args)
	}
	for i, a := range args {
		if a == "-p" && i+1 < len(args) && strings.Contains(args[i+1], resolvedExtra) {
			t.Fatal("the extra path is INTERPOLATED into the profile text; a path " +
				"containing quotes or parens could rewrite the policy. Pass it with -D " +
				"like ROOT.")
		}
	}
}
