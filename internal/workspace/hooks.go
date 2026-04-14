package workspace

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopyHooks copies the operator's .git/hooks/* into the mirror's
// hooks/ directory. Preserves the executable bit so a repo's
// pre-commit / pre-push still runs inside a worktree.
//
// The mirror is a bare clone so its hooks live under MirrorGit/hooks,
// not MirrorGit/.git/hooks.
//
// If the operator's hooks dir is missing, this is a no-op.
func CopyHooks(repoPath, mirrorGit string) error {
	src := filepath.Join(repoPath, ".git", "hooks")
	dst := filepath.Join(mirrorGit, "hooks")

	entries, err := os.ReadDir(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}

	// Hooks dir must be 0o755 — git requires the dir be traversable by
	// the user running git. 0o700 would still work in theory but 0o755
	// matches upstream git's default and avoids cross-user surprises in
	// shared-machine setups.
	if err := os.MkdirAll(dst, 0o755); err != nil { //nolint:gosec // intentional 0o755 for git hooks dir
		return fmt.Errorf("mkdir %s: %w", dst, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// Skip the .sample files git ships by default — those aren't
		// the operator's customized hooks.
		name := e.Name()
		if len(name) > 7 && name[len(name)-7:] == ".sample" {
			continue
		}
		if err := copyHookFile(
			filepath.Join(src, name),
			filepath.Join(dst, name),
		); err != nil {
			return err
		}
	}
	return nil
}

// copyHookFile copies one hook, preserving executable bit.
func copyHookFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}

	in, err := os.Open(src) //nolint:gosec // workspace manages its own paths
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	// 0o600 initial, fixed up below via Chmod.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) //nolint:gosec // dst is workspace-owned, built from mirrorGit + hook name
	if err != nil {
		return fmt.Errorf("open %s for write: %w", dst, err)
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s → %s: %w", src, dst, err)
	}

	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return fmt.Errorf("chmod %s: %w", dst, err)
	}
	return nil
}
