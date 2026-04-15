package workspace

import (
	"context"
	"os"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/variant"
)

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
