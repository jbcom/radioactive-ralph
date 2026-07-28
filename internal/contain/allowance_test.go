//go:build darwin || linux

package contain

import (
	"errors"
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

// TestIsAncestorExcludesEqualPaths pins the clause a review suggested removing.
//
// filepath.Rel(x, x) returns ".", so without the `rel != "."` guard isAncestor
// would report a path as its own ancestor. That matters here because
// resolveExtraWritable asks "does this grant CONTAIN the home directory" -- and
// a self-match would make every path look like it contains itself, which is
// both wrong and the opposite of the protection.
//
// The equality case is ALSO handled before isAncestor is reached (cleaned is
// compared against home and resolvedHome directly), so $HOME is rejected by two
// independent checks. Pinning both: a later refactor that removes one should
// not silently rely on the other still being there.
func TestIsAncestorExcludesEqualPaths(t *testing.T) {
	dir := t.TempDir()
	if isAncestor(dir, dir) {
		t.Fatal("isAncestor(x, x) = true; a path is not its own ancestor, and " +
			"treating it as one inverts the breadth check that uses it")
	}
	if !isAncestor(filepath.Dir(dir), dir) {
		t.Fatalf("isAncestor(%q, %q) = false; a real parent must be recognised or "+
			"the breadth check stops rejecting anything", filepath.Dir(dir), dir)
	}
	if isAncestor(dir, filepath.Dir(dir)) {
		t.Fatal("isAncestor(child, parent) = true; the direction is reversed")
	}
}

// TestHomeIsRejectedByBothChecks pins that $HOME cannot be granted, and does it
// through the PUBLIC path rather than the helper, since that is what callers
// actually reach.
func TestHomeIsRejectedByBothChecks(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	_, err = NewPolicy(t.TempDir(), home)
	if err == nil {
		t.Fatal("$HOME was accepted as an extra writable path; granting it satisfies " +
			"every provider and makes containment vacuous while the config still " +
			"claims a boundary")
	}
	if !errors.Is(err, ErrExtraPathTooBroad) {
		t.Fatalf("NewPolicy($HOME) = %v, want ErrExtraPathTooBroad", err)
	}
}

// TestSymlinkedAllowanceIsRejectedByItsTARGET pins that the breadth check
// follows the link.
//
// A symlink named something innocuous can point at $HOME or "/". Validating
// only the declared spelling let a binding grant the entire home directory
// through a harmless-looking path: the RESOLVED target is what reaches the
// Seatbelt subpath grant and the Landlock rule, so it is what must be checked.
//
// Verified before fixing: NewPolicy(root, link->$HOME) returned nil error with
// ExtraWritable = [/Users/<me>].
func TestSymlinkedAllowanceIsRejectedByItsTARGET(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	link := filepath.Join(t.TempDir(), "innocent-looking")
	if err := os.Symlink(home, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	p, err := NewPolicy(t.TempDir(), link)
	if err == nil {
		t.Fatalf("a symlink to $HOME was accepted, granting %v; the check validated "+
			"the link's spelling rather than the target the kernel actually enforces",
			p.ExtraWritable)
	}
	if !errors.Is(err, ErrExtraPathTooBroad) {
		t.Fatalf("NewPolicy(symlink->$HOME) = %v, want ErrExtraPathTooBroad", err)
	}
}

// TestSymlinkedNarrowAllowanceStillWorks keeps the target check from rejecting
// the ordinary case: a link to a legitimate narrow directory must resolve and
// be granted, or the resolution step would break real configs.
func TestSymlinkedNarrowAllowanceStillWorks(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	p, err := NewPolicy(t.TempDir(), link)
	if err != nil {
		t.Fatalf("a symlink to a NARROW directory was rejected: %v", err)
	}
	resolved, _ := filepath.EvalSymlinks(target)
	if len(p.ExtraWritable) != 1 || p.ExtraWritable[0] != resolved {
		t.Fatalf("ExtraWritable = %v, want [%s]", p.ExtraWritable, resolved)
	}
}
