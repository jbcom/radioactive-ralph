// Package projectid computes a directory's project identity signals.
//
// This lives outside internal/store on purpose. The signals are derived from
// the caller's own filesystem — an absolute path and two git facts — so a
// client may compute them without reading the supervisor-owned database. Before
// this package existed, the only implementation lived in store, which forced
// every client that needed to identify its own directory to import store and
// therefore to open SQLite: exactly the dumb-client violation issue #204 is
// about.
//
// The store still owns what these signals MEAN (matching them against
// accumulated project identifiers); this package only produces them.
package projectid

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Fingerprint kinds. A project accumulates several over its life, so a
// directory that later gains a git remote still resolves to the same project.
const (
	KindAbsPath       = "abs_path"
	KindGitRootCommit = "git_root_commit"
	KindGitRemote     = "git_remote"
)

// Fingerprint is one identity signal for a project.
type Fingerprint struct {
	Kind  string
	Value string
}

// Compute returns the identity signals for dir, most stable last. The absolute
// path is always present; the git signals are added only when dir is inside a
// work tree, and a failure to read either one is not an error — a repo without
// an origin remote is perfectly normal.
func Compute(ctx context.Context, dir string) ([]Fingerprint, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("projectid: abs path: %w", err)
	}
	abs = filepath.Clean(abs)

	fps := []Fingerprint{{Kind: KindAbsPath, Value: abs}}

	if !isGitRepo(ctx, abs) {
		return fps, nil
	}

	if sha, err := runGit(ctx, abs, "rev-list", "--max-parents=0", "HEAD"); err == nil {
		if sha = strings.TrimSpace(sha); sha != "" {
			// A repo can have multiple root commits (unlikely, but rev-list can
			// print more than one). Use the first line only.
			if nl := strings.IndexByte(sha, '\n'); nl >= 0 {
				sha = sha[:nl]
			}
			fps = append(fps, Fingerprint{Kind: KindGitRootCommit, Value: sha})
		}
	}

	if remote, err := runGit(ctx, abs, "remote", "get-url", "origin"); err == nil {
		if remote = strings.TrimSpace(remote); remote != "" {
			fps = append(fps, Fingerprint{Kind: KindGitRemote, Value: remote})
		}
	}

	return fps, nil
}

// isGitRepo reports whether dir is inside a git working tree, via
// `git rev-parse --is-inside-work-tree` rather than a bare .git stat so
// worktrees and submodules are recognized correctly.
func isGitRepo(ctx context.Context, dir string) bool {
	out, err := runGit(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	//nolint:gosec // G204: args are hardcoded git subcommands/flags from this file, not user input
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
