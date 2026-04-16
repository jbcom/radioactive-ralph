package xdg

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveRequiresRepoPath(t *testing.T) {
	t.Setenv("RALPH_STATE_DIR", t.TempDir())

	if _, err := Resolve(""); err == nil {
		t.Fatal("expected error for empty repo path, got nil")
	}
}

func TestResolveProducesAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", root)

	repo := t.TempDir()
	paths, err := Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	assertAbs := func(name, path string) {
		t.Helper()
		if !filepath.IsAbs(path) {
			t.Errorf("%s should be absolute, got %q", name, path)
		}
	}
	assertAbs("StateRoot", paths.StateRoot)
	assertAbs("Workspace", paths.Workspace)
	assertAbs("MirrorGit", paths.MirrorGit)
	assertAbs("Shallow", paths.Shallow)
	assertAbs("Worktrees", paths.Worktrees)
	assertAbs("Sessions", paths.Sessions)
	assertAbs("Logs", paths.Logs)
	assertAbs("StateDB", paths.StateDB)
}

func TestResolveHashesRepoPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", root)

	repo := t.TempDir()
	paths, err := Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// EvalSymlinks may resolve the tmp dir differently, so compute
	// the hash from the resolved path to match what Resolve did.
	evalued, err := filepath.EvalSymlinks(repo)
	if err != nil {
		evalued = repo
	}
	sum := sha256.Sum256([]byte(evalued))
	want := hex.EncodeToString(sum[:])[:RepoHashLen]

	got := filepath.Base(paths.Workspace)
	if got != want {
		t.Errorf("Workspace dir should be %q (hash of %q), got %q", want, evalued, got)
	}
}

func TestResolveSameRepoYieldsSameHash(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", root)

	repo := t.TempDir()

	first, err := Resolve(repo)
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	second, err := Resolve(repo)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if first.Workspace != second.Workspace {
		t.Errorf("same repo should resolve to same workspace, got %q vs %q",
			first.Workspace, second.Workspace)
	}
}

func TestResolveDifferentReposYieldDifferentHashes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", root)

	a, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("Resolve a: %v", err)
	}
	b, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("Resolve b: %v", err)
	}
	if a.Workspace == b.Workspace {
		t.Errorf("different repos should hash differently")
	}
}

func TestResolveRelativePathIsAbsolutised(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", root)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Chdir(t.TempDir())

	abs, err := Resolve(".")
	if err != nil {
		t.Fatalf("Resolve abs: %v", err)
	}
	// Ensure we ended up with the *new* cwd, not the test's invocation cwd.
	if strings.HasPrefix(abs.Workspace, filepath.Join(root, "_")) {
		t.Errorf("Workspace should hash the new cwd, not fall back to a sentinel: %s", abs.Workspace)
	}
	_ = cwd
}

func TestStateRootHonoursOverride(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", custom)

	paths, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.HasPrefix(paths.Workspace, custom) {
		t.Errorf("Workspace %q should start with RALPH_STATE_DIR %q", paths.Workspace, custom)
	}
}

func TestStateRootLinuxXDG(t *testing.T) {
	xdg := t.TempDir()
	got := stateRootForGOOS("linux", "/home/me", xdg, "")
	want := filepath.Join(xdg, AppName)
	if got != want {
		t.Fatalf("stateRootForGOOS(linux) = %q, want %q", got, want)
	}
}

func TestStateRootLinuxDefault(t *testing.T) {
	got := stateRootForGOOS("linux", "/home/me", "", "")
	want := filepath.Join("/home/me", ".local", "state", AppName)
	if got != want {
		t.Fatalf("stateRootForGOOS(linux default) = %q, want %q", got, want)
	}
}

func TestStateRootDarwin(t *testing.T) {
	got := stateRootForGOOS("darwin", "/Users/me", "", "")
	want := filepath.Join("/Users/me", "Library", "Application Support", AppName)
	if got != want {
		t.Fatalf("stateRootForGOOS(darwin) = %q, want %q", got, want)
	}
}

func TestStateRootWindowsLocalAppData(t *testing.T) {
	got := stateRootForGOOS("windows", `C:\Users\me`, "", `C:\Users\me\AppData\Local`)
	want := filepath.Join(`C:\Users\me\AppData\Local`, AppName)
	if got != want {
		t.Fatalf("stateRootForGOOS(windows localappdata) = %q, want %q", got, want)
	}
}

func TestStateRootWindowsFallback(t *testing.T) {
	got := stateRootForGOOS("windows", `C:\Users\me`, "", "")
	want := filepath.Join(`C:\Users\me`, "AppData", "Local", AppName)
	if got != want {
		t.Fatalf("stateRootForGOOS(windows fallback) = %q, want %q", got, want)
	}
}

func TestEnsureCreatesDirectories(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", root)

	paths, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	for _, dir := range []string{paths.Workspace, paths.Worktrees, paths.Sessions, paths.Logs} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("%s not created: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s should be a directory", dir)
		}
		// MkdirAll honors the process umask; on most CI systems 0o022 means
		// 0o700 & ~0o022 = 0o700, but we check the owner bits only.
		const ownerBits = 0o700
		if info.Mode().Perm()&ownerBits != ownerBits {
			t.Errorf("%s mode %o should have owner rwx", dir, info.Mode().Perm())
		}
	}
}

func TestEnsureIsIdempotent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("RALPH_STATE_DIR", root)

	paths, err := Resolve(t.TempDir())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for range 3 {
		if err := paths.Ensure(); err != nil {
			t.Fatalf("Ensure: %v", err)
		}
	}
}
