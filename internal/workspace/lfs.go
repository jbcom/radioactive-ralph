package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// HasLFS reports whether repoPath has `.gitattributes` entries that
// reference git-lfs filters. Used by pre-flight to decide whether LFS
// knob choices matter at all.
func HasLFS(repoPath string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(repoPath, ".gitattributes")) //nolint:gosec // explicit workspace path
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read .gitattributes: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Canonical LFS entry: `*.bin filter=lfs diff=lfs merge=lfs -text`.
		if strings.Contains(line, "filter=lfs") {
			return true, nil
		}
	}
	return false, nil
}

// applyLFSConfig writes git config keys into the target dir (mirror or
// shallow) matching the resolved LFSMode. Does nothing when the repo
// has no LFS content.
//
// Mappings:
//
//   - LFSFull           — lfs.fetchexclude = "" (fetch everything)
//   - LFSOnDemand       — lfs.fetchexclude = "*" (skip on clone; fetch
//     per-path when task requests)
//   - LFSPointersOnly   — lfs.fetchexclude = "*" AND lfs.hooksInstalled
//     = false so pointers are never resolved
//   - LFSExcluded       — pre-flight check refuses the task; nothing to
//     configure here (supervisor handles refusal)
func (m *Manager) applyLFSConfig(ctx context.Context, target string) error {
	hasLFS, err := HasLFS(m.RepoPath)
	if err != nil {
		return err
	}
	if !hasLFS {
		return nil
	}

	switch m.LFS {
	case variant.LFSFull:
		return runGit(ctx, target, "config", "lfs.fetchexclude", "")
	case variant.LFSOnDemand:
		return runGit(ctx, target, "config", "lfs.fetchexclude", "*")
	case variant.LFSPointersOnly:
		if err := runGit(ctx, target, "config", "lfs.fetchexclude", "*"); err != nil {
			return err
		}
		return runGit(ctx, target, "config", "lfs.hooksInstalled", "false")
	case variant.LFSExcluded:
		// Supervisor refuses tasks that touch LFS paths; no config needed.
		return nil
	default:
		return fmt.Errorf("unknown LFS mode %q", m.LFS)
	}
}
