package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/variant"
)

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
