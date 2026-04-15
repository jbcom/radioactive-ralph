package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// Shared helpers used across isolation_test.go, worktree_test.go, and
// content_test.go.

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

// mustMgr constructs a Manager with profile-defaulted knobs.
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

// contains is a tiny substring check used by the worktree-list assertion
// in the reconcile test.
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
