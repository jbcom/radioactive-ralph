package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

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
