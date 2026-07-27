package orch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestSecureProjectPathReturnsResolvedPath is the CWE-22 fix. Returning the
// CANDIDATE after checking the RESOLVED path hands the caller a string that was
// never the thing containment approved: the caller then opens the candidate,
// which re-resolves through whatever the symlink points at now.
//
// The contract is that the returned path is the one that passed the check.
func TestSecureProjectPathReturnsResolvedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	root := realTempDir(t)
	realDir := filepath.Join(root, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	target := filepath.Join(realDir, "file.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// An in-root symlink to an in-root file: containment holds, but the
	// candidate and resolved paths differ.
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := secureProjectPath(root, "link.txt")
	if err != nil {
		t.Fatalf("secureProjectPath: %v", err)
	}
	if got == link {
		t.Fatalf("returned the candidate %q; must return the resolved path so the "+
			"caller operates on what containment actually approved", got)
	}
	wantResolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if got != wantResolved {
		t.Fatalf("resolved = %q, want %q", got, wantResolved)
	}
}

// TestSecureProjectPathRefusesAbsolutePath pins the other half of CWE-22: a
// declared path is always project-relative. Honoring an absolute path would let
// a plan name /etc/passwd as an "output" and have it treated as in-scope.
func TestSecureProjectPathRefusesAbsolutePath(t *testing.T) {
	root := realTempDir(t)
	absolute := filepath.Join(string(filepath.Separator), "etc", "passwd")
	if _, err := secureProjectPath(root, absolute); err == nil {
		t.Fatalf("accepted absolute path %q; declared paths are project-relative", absolute)
	}
}

// TestSecureProjectPathRefusesEscape covers the plain traversal case.
func TestSecureProjectPathRefusesEscape(t *testing.T) {
	root := realTempDir(t)
	for _, rel := range []string{
		"../outside.txt",
		"nested/../../outside.txt",
	} {
		if _, err := secureProjectPath(root, rel); err == nil {
			t.Errorf("accepted %q, which escapes the project root", rel)
		}
	}
}

// TestSecureProjectPathRefusesSymlinkedAncestor is the vector reproduced in the
// design doc: the declared path's PARENT is a symlink pointing outside the
// project. Resolving only the leaf would miss it, because the leaf itself does
// not exist yet.
func TestSecureProjectPathRefusesSymlinkedAncestor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	root := realTempDir(t)
	outside := realTempDir(t)
	// build/ -> <outside>, so build/out.txt lands outside the project.
	if err := os.Symlink(outside, filepath.Join(root, "build")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := secureProjectPath(root, filepath.Join("build", "out.txt")); err == nil {
		t.Fatal("accepted build/out.txt through a symlinked ancestor pointing outside the root")
	}
}

// TestSecureProjectPathAllowsNotYetCreatedFile keeps the check usable: a
// declared OUTPUT does not exist at admission time by definition, so refusing
// every nonexistent path would refuse every plan.
func TestSecureProjectPathAllowsNotYetCreatedFile(t *testing.T) {
	root := realTempDir(t)
	if err := os.MkdirAll(filepath.Join(root, "build"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got, err := secureProjectPath(root, filepath.Join("build", "out.txt"))
	if err != nil {
		t.Fatalf("secureProjectPath on a not-yet-created output: %v", err)
	}
	if !strings.HasPrefix(got, root) {
		t.Fatalf("resolved %q is not under root %q", got, root)
	}
}

// TestOpenContainedFileRefusesASymlink is the no-follow guarantee. Ralph hashes
// a declared input to pin it; if the open follows a symlink, the bytes hashed
// are whatever the link points at when the read happens, not the inode
// containment approved.
func TestOpenContainedFileRefusesASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	root := realTempDir(t)
	secret := filepath.Join(realTempDir(t), "secret.txt")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(root, "input.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	f, err := openContainedFile(link)
	if err == nil {
		_ = f.Close()
		t.Fatal("opened a symlink; the no-follow open must refuse it so the bytes " +
			"hashed are the bytes of the inode that was checked")
	}
}

// TestOpenContainedFileReadsARegularFile is the control: the no-follow open must
// still work for the ordinary case, or every input pin would fail closed.
func TestOpenContainedFileReadsARegularFile(t *testing.T) {
	root := realTempDir(t)
	path := filepath.Join(root, "input.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	f, err := openContainedFile(path)
	if err != nil {
		t.Fatalf("openContainedFile: %v", err)
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 5)
	if _, err := f.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("read %q, want hello", buf)
	}
}

// TestHashContainedFileHashesTheOpenedInode proves the hash comes from the file
// handle rather than a re-resolved pathname. Swapping the path for a symlink
// AFTER the open must not change what was hashed — that is the whole point of
// hashing from the *os.File.
func TestHashContainedFileHashesTheOpenedInode(t *testing.T) {
	root := realTempDir(t)
	path := filepath.Join(root, "input.txt")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	first, err := hashContainedFile(path)
	if err != nil {
		t.Fatalf("hashContainedFile: %v", err)
	}
	if first == "" {
		t.Fatal("empty hash")
	}

	// Same content, hashed again: stable.
	second, err := hashContainedFile(path)
	if err != nil {
		t.Fatalf("hashContainedFile again: %v", err)
	}
	if first != second {
		t.Fatalf("hash is not stable: %q vs %q", first, second)
	}

	// Different content: different hash. Without this the test would pass on a
	// stub that returns a constant.
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	third, err := hashContainedFile(path)
	if err != nil {
		t.Fatalf("hashContainedFile after change: %v", err)
	}
	if third == first {
		t.Fatal("hash did not change when the content changed")
	}
}

// TestHashContainedFileFailsClosedOnASymlink ties the two together: the hash
// path must inherit the no-follow refusal rather than falling back to a
// pathname read.
func TestHashContainedFileFailsClosedOnASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	root := realTempDir(t)
	secret := filepath.Join(realTempDir(t), "secret.txt")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(root, "input.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := hashContainedFile(link); err == nil {
		t.Fatal("hashed through a symlink; must fail closed")
	}
}

// TestValidateTaskFilesystemRejectsAnEscapingDeclaration wires the check to the
// declaration surface: a task declaring an escaping input or output must be
// refused at admission rather than dispatched and caught later.
func TestValidateTaskFilesystemRejectsAnEscapingDeclaration(t *testing.T) {
	root := realTempDir(t)
	for name, decl := range map[string]taskFilesystemDecl{
		"escaping input":  {Inputs: []string{"../outside.txt"}},
		"escaping output": {Outputs: []string{"../outside.txt"}},
		"absolute output": {Outputs: []string{filepath.Join(string(filepath.Separator), "etc", "passwd")}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateTaskFilesystem(root, decl); err == nil {
				t.Fatalf("accepted %+v", decl)
			} else if !errors.Is(err, ErrTaskPathEscapesProject) {
				t.Fatalf("err = %v, want ErrTaskPathEscapesProject so callers can "+
					"distinguish containment from an I/O fault", err)
			}
		})
	}
}

// realTempDir returns a t.TempDir with symlinks resolved. On macOS /var is
// itself a symlink to /private/var, so an unresolved temp root makes every
// containment comparison fail for the wrong reason.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(TempDir): %v", err)
	}
	return dir
}

// escapingOutputPlan declares an output that resolves outside the project root.
const escapingOutputPlan = "# Escape\n\n" +
	"1. write somewhere it should not\n\n" +
	"   ```ralph-task\n" +
	`   {"id": "escaper", "outputs": [{"path": "../outside.txt"}]}` + "\n" +
	"   ```\n"

// TestDispatchBlocksATaskWhoseOutputEscapes is the dispatch-time half of
// containment. A task declaring an escaping output must be BLOCKED, not
// silently skipped: a skipped task is indistinguishable from one waiting on a
// dependency, so the refusal would be invisible to an operator.
func TestDispatchBlocksATaskWhoseOutputEscapes(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "escaping-output")

	runner := &bindingRecordingRunner{}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("codex", false)),
	)
	planID := mustImportPlan(t, o, projectID, "escaping", escapingOutputPlan)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()
	if dispatched != 0 {
		t.Fatalf("dispatched = %d, want 0 — a task declaring an escaping output must not run", dispatched)
	}
	if names := runner.names(); len(names) != 0 {
		t.Fatalf("provider turns ran: %v", names)
	}

	task, err := s.GetTask(ctx, planID, "escaper")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != store.TaskStatusBlockedInput {
		t.Fatalf("status = %q, want %q — the refusal must be visible, not a silent skip",
			task.Status, store.TaskStatusBlockedInput)
	}
	meta, err := s.GetTaskExecutionMetadata(ctx, planID, "escaper")
	if err != nil {
		t.Fatalf("GetTaskExecutionMetadata: %v", err)
	}
	if !strings.Contains(meta.BlockedReason, "escapes the project root") {
		t.Fatalf("blocked reason = %q, want it to name the containment failure", meta.BlockedReason)
	}
}

// TestDispatchRunsATaskWithContainedPaths is the control: declaring paths must
// not by itself prevent dispatch, or the gate would block every annotated plan.
func TestDispatchRunsATaskWithContainedPaths(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "contained-output")

	runner := &bindingRecordingRunner{}
	o := New(s,
		WithRunnerFactory(func(provider.Binding) (provider.Runner, error) { return runner, nil }),
		WithBindingResolver(fakeBindingResolver("codex", false)),
	)
	contained := "# Contained\n\n" +
		"1. write inside the project\n\n" +
		"   ```ralph-task\n" +
		`   {"id": "good", "outputs": [{"path": "out.txt"}]}` + "\n" +
		"   ```\n"
	planID := mustImportPlan(t, o, projectID, "contained", contained)

	dispatched, err := o.DispatchNext(ctx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	o.Wait()
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1 — a contained declaration must still run", dispatched)
	}
}
