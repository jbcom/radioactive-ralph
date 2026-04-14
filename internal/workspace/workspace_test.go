package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// newTestRepo creates a throwaway git repo under t.TempDir() with one
// commit so clone/fetch operations have something to pull.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q", "-b", "main")
	mustRun(t, dir, "git", "config", "user.email", "ralph@example.com")
	mustRun(t, dir, "git", "config", "user.name", "Ralph")
	mustRun(t, dir, "git", "config", "commit.gpgsign", "false")

	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustRun(t, dir, "git", "add", "README.md")
	mustRun(t, dir, "git", "commit", "-q", "-m", "init")
	return dir
}

func mustRun(t *testing.T, cwd, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// isolateState overrides RALPH_STATE_DIR so the test workspace lives
// under a throwaway tmp dir.
func isolateState(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", dir)
}

func mustMgr(t *testing.T, repo string, iso variant.IsolationMode, p variant.Profile) *Manager {
	t.Helper()
	obj := p.ObjectStoreDefault
	if obj == "" {
		obj = variant.ObjectStoreReference
	}
	sync := p.SyncSourceDefault
	if sync == "" {
		sync = variant.SyncSourceBoth
	}
	lfs := p.LFSModeDefault
	if lfs == "" {
		lfs = variant.LFSOnDemand
	}
	m, err := New(repo, p, iso, obj, sync, lfs)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// ── Isolation dispatch tests ------------------------------------------

func TestInitSharedCreatesNoMirror(t *testing.T) {
	isolateState(t)
	repo := newTestRepo(t)
	p, _ := variant.Lookup("blue")
	m := mustMgr(t, repo, variant.IsolationShared, p)

	if err := m.Init(context.Background()); err != nil {
		t.Fatalf("Init(shared): %v", err)
	}
	if _, err := os.Stat(m.Paths.MirrorGit); !os.IsNotExist(err) {
		t.Errorf("shared must not create mirror.git, got err=%v", err)
	}
	// State dirs should be created (for logs/sockets/db).
	if _, err := os.Stat(m.Paths.Workspace); err != nil {
		t.Errorf("shared must create workspace state dir: %v", err)
	}
}

func TestInitShallowClonesDepthOne(t *testing.T) {
	isolateState(t)
	repo := newTestRepo(t)
	p, _ := variant.Lookup("green")
	m := mustMgr(t, repo, variant.IsolationShallow, p)

	if err := m.Init(context.Background()); err != nil {
		t.Fatalf("Init(shallow): %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.Paths.Shallow, ".git")); err != nil {
		t.Errorf("shallow clone missing: %v", err)
	}
	if _, err := os.Stat(m.Paths.MirrorGit); !os.IsNotExist(err) {
		t.Errorf("shallow must not create mirror.git")
	}
}

func TestInitMirrorCreatesBareClone(t *testing.T) {
	isolateState(t)
	repo := newTestRepo(t)
	p, _ := variant.Lookup("green")
	m := mustMgr(t, repo, variant.IsolationMirrorSingle, p)

	if err := m.Init(context.Background()); err != nil {
		t.Fatalf("Init(mirror): %v", err)
	}
	// Bare clone has HEAD at the repo root, no .git subdir.
	if _, err := os.Stat(filepath.Join(m.Paths.MirrorGit, "HEAD")); err != nil {
		t.Errorf("mirror.git/HEAD missing: %v", err)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	isolateState(t)
	repo := newTestRepo(t)
	p, _ := variant.Lookup("green")
	m := mustMgr(t, repo, variant.IsolationMirrorSingle, p)
	ctx := context.Background()

	if err := m.Init(ctx); err != nil {
		t.Fatalf("Init #1: %v", err)
	}
	if err := m.Init(ctx); err != nil {
		t.Errorf("Init #2 should be a no-op: %v", err)
	}
}

func TestInitRejectsNonGitRepo(t *testing.T) {
	isolateState(t)
	bogus := t.TempDir()
	p, _ := variant.Lookup("green")
	m := mustMgr(t, bogus, variant.IsolationMirrorSingle, p)
	err := m.Init(context.Background())
	if err == nil {
		t.Fatal("expected error when RepoPath is not a git repo")
	}
}

// ── Worktree lifecycle -----------------------------------------------

func TestAcquireReleaseWorktree(t *testing.T) {
	isolateState(t)
	repo := newTestRepo(t)
	p, _ := variant.Lookup("green")
	m := mustMgr(t, repo, variant.IsolationMirrorPool, p)
	ctx := context.Background()

	if err := m.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	wt, err := m.AcquireWorktree(ctx)
	if err != nil {
		t.Fatalf("AcquireWorktree: %v", err)
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Errorf("worktree path missing: %v", err)
	}
	if wt.Branch == "" {
		t.Error("worktree must have a branch")
	}

	if err := m.ReleaseWorktree(ctx, wt); err != nil {
		t.Errorf("ReleaseWorktree: %v", err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Errorf("worktree path should be gone: err=%v", err)
	}
}

func TestAcquirePoolFull(t *testing.T) {
	isolateState(t)
	repo := newTestRepo(t)
	// Use grey which caps at 1 parallel worktree.
	p, _ := variant.Lookup("grey")
	m := mustMgr(t, repo, variant.IsolationMirrorSingle, p)
	ctx := context.Background()

	if err := m.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	wt, err := m.AcquireWorktree(ctx)
	if err != nil {
		t.Fatalf("Acquire #1: %v", err)
	}
	if _, err := m.AcquireWorktree(ctx); err == nil {
		t.Fatal("Acquire #2 should fail — pool is full")
	}
	if err := m.ReleaseWorktree(ctx, wt); err != nil {
		t.Errorf("Release: %v", err)
	}
	if _, err := m.AcquireWorktree(ctx); err != nil {
		t.Errorf("Acquire after release should succeed: %v", err)
	}
}

func TestReconcileAfterStaleWorktreeDir(t *testing.T) {
	isolateState(t)
	repo := newTestRepo(t)
	p, _ := variant.Lookup("green")
	m := mustMgr(t, repo, variant.IsolationMirrorPool, p)
	ctx := context.Background()

	if err := m.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}
	wt, err := m.AcquireWorktree(ctx)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// Simulate an ungraceful shutdown — remove the worktree dir from
	// disk without telling git.
	if err := os.RemoveAll(wt.Path); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if err := m.Reconcile(ctx); err != nil {
		t.Errorf("Reconcile: %v", err)
	}
	// After reconcile, git worktree list should no longer include the
	// phantom worktree.
	out, err := gitOutput(ctx, m.Paths.MirrorGit, "worktree", "list")
	if err != nil {
		t.Fatalf("worktree list: %v", err)
	}
	if _, err := os.Stat(wt.Path); err == nil {
		t.Errorf("stale worktree dir still present: %s", wt.Path)
	}
	// Admin state for the removed path should be pruned.
	if contains(out, wt.Path) {
		t.Errorf("worktree list still references pruned path: %s\n%s", wt.Path, out)
	}
}

// ── LFS detection ----------------------------------------------------

func TestHasLFSTrueWhenGitattributesHasFilterLFS(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"),
		[]byte("*.bin filter=lfs diff=lfs merge=lfs -text\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}
	has, err := HasLFS(repo)
	if err != nil {
		t.Fatalf("HasLFS: %v", err)
	}
	if !has {
		t.Error("expected LFS=true with filter=lfs line")
	}
}

func TestHasLFSFalseWhenNoLFSLine(t *testing.T) {
	repo := newTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"),
		[]byte("* text=auto\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}
	has, err := HasLFS(repo)
	if err != nil {
		t.Fatalf("HasLFS: %v", err)
	}
	if has {
		t.Error("expected LFS=false without filter=lfs")
	}
}

func TestHasLFSFalseWhenFileMissing(t *testing.T) {
	repo := newTestRepo(t)
	has, err := HasLFS(repo)
	if err != nil {
		t.Fatalf("HasLFS: %v", err)
	}
	if has {
		t.Error("expected LFS=false when .gitattributes missing")
	}
}

// ── Hook copy --------------------------------------------------------

func TestCopyHooksPreservesExecutableBit(t *testing.T) {
	repo := newTestRepo(t)
	hookSrc := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hookSrc, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	mirror := t.TempDir()
	if err := CopyHooks(repo, mirror); err != nil {
		t.Fatalf("CopyHooks: %v", err)
	}
	info, err := os.Stat(filepath.Join(mirror, "hooks", "pre-commit"))
	if err != nil {
		t.Fatalf("stat copied hook: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("copied hook lost executable bit: mode=%v", info.Mode().Perm())
	}
}

func TestCopyHooksSkipsSampleFiles(t *testing.T) {
	repo := newTestRepo(t)
	sample := filepath.Join(repo, ".git", "hooks", "pre-commit.sample")
	if err := os.WriteFile(sample, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	mirror := t.TempDir()
	if err := CopyHooks(repo, mirror); err != nil {
		t.Fatalf("CopyHooks: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mirror, "hooks", "pre-commit.sample")); err == nil {
		t.Error("sample file should not have been copied")
	}
}

func TestCopyHooksMissingSourceIsNoOp(t *testing.T) {
	missing := t.TempDir() // no .git
	mirror := t.TempDir()
	if err := CopyHooks(missing, mirror); err != nil {
		t.Errorf("CopyHooks on missing hooks dir should be no-op, got %v", err)
	}
}

// ── helper -----------------------------------------------------------

func contains(haystack, needle string) bool {
	return len(haystack) > 0 && len(needle) > 0 &&
		(haystack == needle || (len(haystack) >= len(needle) &&
			indexOf(haystack, needle) >= 0))
}

func indexOf(s, sub string) int {
	n := len(sub)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == sub {
			return i
		}
	}
	return -1
}
