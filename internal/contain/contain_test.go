package contain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestPolicyRefusesARelativeRoot fails closed on a root that is not absolute.
//
// A relative root would be resolved against whatever working directory the
// provider process happens to inherit, so the boundary would name a different
// directory than the caller meant — a containment policy that silently guards
// the wrong tree is worse than none, because it reports success.
func TestPolicyRefusesARelativeRoot(t *testing.T) {
	if _, err := NewPolicy("relative/path"); !errors.Is(err, ErrRootNotAbsolute) {
		t.Fatalf("err = %v, want ErrRootNotAbsolute", err)
	}
}

// TestPolicyResolvesSymlinksInTheRoot pins the boundary to the REAL directory.
//
// A policy written against a symlinked path would name the link, not its
// target, so a provider writing through the resolved path would fall outside a
// boundary that appears to contain it.
func TestPolicyResolvesSymlinksInTheRoot(t *testing.T) {
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}
	p, err := NewPolicy(link)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	wantReal, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if p.Root != wantReal {
		t.Fatalf("Root = %q, want the resolved target %q", p.Root, wantReal)
	}
}

// TestWriteInsideTheRootSucceeds is the control: containment that blocked the
// provider's own working tree would make every task fail.
func TestWriteInsideTheRootSucceeds(t *testing.T) {
	requireEnforcement(t)
	root := t.TempDir()
	target := filepath.Join(root, "inside.txt")

	if err := runContained(t, root, "echo ok > "+shellQuote(target)); err != nil {
		t.Fatalf("a write inside the root was refused: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("inside write did not land: %v", err)
	}
}

// TestWriteOutsideTheRootIsRefusedByTheKernel is the whole point of this
// package, and the difference from secureProjectPath: that validates strings
// Ralph handles, this stops the PROVIDER PROCESS's syscall. No string Ralph
// returns travels into that process, so only a kernel-enforced boundary makes
// the guarantee real.
func TestWriteOutsideTheRootIsRefusedByTheKernel(t *testing.T) {
	requireEnforcement(t)
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escaped.txt")

	err := runContained(t, root, "echo escaped > "+shellQuote(outside))
	if err == nil {
		t.Fatal("a write outside the root succeeded; the containment does not contain")
	}
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Fatal("the escaping file exists; the write was not actually prevented")
	}
}

// TestContainmentIsInheritedByChildProcesses is load-bearing rather than
// incidental: a fan-out provider spawns its own sub-agents, so a boundary that
// stopped only the top-level process would be trivially escaped by the exact
// providers Ralph is built to run.
func TestContainmentIsInheritedByChildProcesses(t *testing.T) {
	requireEnforcement(t)
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "child-escaped.txt")

	// A grandchild, not just a child.
	script := "/bin/sh -c " + shellQuote("echo child > "+shellQuote(outside))
	if err := runContained(t, root, script); err == nil {
		t.Fatal("a child process wrote outside the root; a fan-out provider's " +
			"sub-agents would escape containment entirely")
	}
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Fatal("the child's escaping file exists")
	}
}

// TestUnavailableContainmentIsReportedNotSilentlySkipped is the fail-closed
// contract. A caller that believes it is contained when it is not would make
// exactly the false guarantee this package exists to replace, so Available()
// must be checked and Wrap must say so rather than returning the command
// unchanged.
func TestUnavailableContainmentIsReportedNotSilentlySkipped(t *testing.T) {
	if Available() {
		t.Skip("containment is available on this platform; nothing to assert here")
	}
	p, err := NewPolicy(t.TempDir())
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	if _, _, err := p.Wrap("/bin/echo", []string{"hi"}); !errors.Is(err, ErrContainmentUnavailable) {
		t.Fatalf("err = %v, want ErrContainmentUnavailable — a caller must never "+
			"believe it is contained when it is not", err)
	}
}

// requireEnforcement skips the behavioral tests where no primitive exists,
// rather than asserting a guarantee the platform cannot make.
func requireEnforcement(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skipf("no write containment primitive on %s", runtime.GOOS)
	}
}

// runContained executes one shell command under the policy and reports whether
// it succeeded.
//
// On platforms whose Wrap re-execs THIS binary as a containment helper (linux),
// the wrapped argv[0] is replaced with a purpose-built helper. Running the go
// test binary instead makes it reject the helper flag and never run the
// command at all — which every "did the file appear?" assertion then reads as
// successful containment. That false pass is the failure mode these tests exist
// to rule out, so it must not be reachable.
func runContained(t *testing.T, root, script string) error {
	t.Helper()
	p, err := NewPolicy(root)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	name, args, err := p.Wrap("/bin/sh", []string{"-c", script})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if selfExec, _ := os.Executable(); name == selfExec {
		name = buildContainHelper(t)
	}
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // test-owned
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("contained command output: %s", out)
	}
	return err
}

// buildContainHelper compiles the stand-in helper command, whose main() calls
// MaybeRunHelper the way the real CLI entry point does.
func buildContainHelper(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "containhelper")
	build := exec.Command("go", "build", "-o", bin,
		"github.com/jbcom/radioactive-ralph/internal/contain/internal/containhelper") //nolint:gosec // test-owned
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build containment helper: %v\n%s", err, out)
	}
	return bin
}

// shellQuote single-quotes s for /bin/sh, escaping any embedded apostrophe.
//
// t.TempDir() paths derive from the TEST NAME, so a test whose name ever
// contains an apostrophe would otherwise produce a command targeting a
// different path — or failing to parse — and the containment assertion would
// read that as a refused write.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// TestPolicyGrantsNothingOutsideTheRoot is a regression for a hole this package
// shipped with in draft and the behavioral tests caught.
//
// The profile granted the resolved TMPDIR so provider scratch files would work.
// On macOS that resolves under /private/tmp, so one convenience line re-opened
// writes to a subtree full of other tools' files — while the policy still
// reported containment. A grant that widens the boundary is worse than no
// containment at all.
//
// It inspects the PROFILE's write-allow rules rather than scanning raw argv:
// the root itself legitimately lives under a temp directory in tests, so a
// substring scan cannot tell the intended grant from an extra one. What must
// hold is that every writable SUBPATH is the root, whatever the root happens
// to be.
func TestPolicyGrantsNothingOutsideTheRoot(t *testing.T) {
	requireEnforcement(t)
	if runtime.GOOS != "darwin" {
		t.Skip("asserts the Seatbelt profile's grants; other platforms encode " +
			"their boundary differently and have their own guards")
	}
	p, err := NewPolicy(t.TempDir())
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	_, args, err := p.Wrap("/bin/echo", []string{"hi"})
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}

	profile, params := "", map[string]string{}
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "-p":
			profile = args[i+1]
		case "-D":
			if k, v, ok := strings.Cut(args[i+1], "="); ok {
				params[k] = v
			}
		}
	}
	if profile == "" {
		t.Fatalf("no profile in argv: %v", args)
	}

	// Every subpath write-grant must resolve to the containment root.
	for _, line := range strings.Split(profile, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "(allow file-write") || !strings.Contains(line, "subpath") {
			continue
		}
		_, after, found := strings.Cut(line, `(param "`)
		name, _, ok := strings.Cut(after, `"`)
		if !found || !ok {
			t.Errorf("subpath grant %q does not bind a parameter; an inlined path "+
				"cannot be audited and may widen the boundary", line)
			continue
		}
		if got := params[name]; got != p.Root {
			t.Errorf("subpath grant binds %s=%q, want the containment root %q — "+
				"a scratch-space grant silently widens the boundary while still "+
				"reporting containment", name, got, p.Root)
		}
	}
	if params["ROOT"] != p.Root {
		t.Errorf("ROOT=%q, want %q", params["ROOT"], p.Root)
	}
}

// TestPolicyRefusesANonDirectoryRoot fails closed on a root that cannot
// represent a writable subtree.
//
// EvalSymlinks happily resolves a regular file, so NewPolicy accepted one and
// produced a policy whose "root" is a single file. What the platform does with
// that is undefined — macOS builds a subpath rule for a non-directory, and
// Landlock opens it O_PATH and grants directory-creation rights beneath a plain
// file. Neither is a boundary anyone reasoned about, and this package's stated
// contract is to fail closed rather than guess.
func TestPolicyRefusesANonDirectoryRoot(t *testing.T) {
	file := filepath.Join(t.TempDir(), "regular.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if _, err := NewPolicy(file); !errors.Is(err, ErrRootNotDirectory) {
		t.Fatalf("err = %v, want ErrRootNotDirectory — a single file cannot be a "+
			"writable subtree, so accepting it yields a boundary nobody defined", err)
	}
}

// TestPolicyRefusesAMissingRoot covers the other invalid shape.
func TestPolicyRefusesAMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := NewPolicy(missing); err == nil {
		t.Fatal("a nonexistent root was accepted; containment cannot be enforced " +
			"against a directory that is not there")
	}
}
