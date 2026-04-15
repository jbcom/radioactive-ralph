package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// fileURL returns a file:// URL for repoPath (absolute, with .git
// extension on the dotgit inside). Git accepts either repo root or
// .git dir as a remote URL, but file:// requires an absolute path.
func fileURL(repoPath string) string {
	return "file://" + repoPath
}

// assertGitRepo returns nil iff path contains a .git entry (file or dir).
func assertGitRepo(path string) error {
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		return fmt.Errorf("not a git repo: %s", path)
	}
	return nil
}

// runGit executes git with the given args, optionally in a specified
// working directory. Inherits parent stderr to surface git's own
// diagnostics on failure.
//
// The args are composed exclusively from workspace-controlled values
// (variant profile knobs, XDG paths, caller-controlled remote names).
// User input never reaches them directly — the supervisor validates
// variant + isolation mode before this function is called.
func runGit(ctx context.Context, cwd string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // args are workspace-controlled
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", // never block on credential prompts
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitOutput runs git and returns stdout as a string. Same security
// posture as runGit — args are workspace-controlled.
func gitOutput(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // args are workspace-controlled
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
