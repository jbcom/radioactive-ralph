// Package workspace manages the on-disk git workspace a variant runs in.
//
// Four orthogonal knobs, resolved from variant profile + config.toml
// overrides with safety-floor precedence:
//
//	isolation     — shared | shallow | mirror-single | mirror-pool
//	object_store  — reference | full  (mirror-* only)
//	sync_source   — local | origin | both
//	lfs_mode      — full | on-demand | pointers-only | excluded
//
// Dispatch:
//
//   - shared        → no mirror, no worktree; variant runs against the operator's repo
//   - shallow       → git clone --depth=1 into StateRoot/<hash>/shallow
//   - mirror-single → git clone --mirror into MirrorGit, one worktree
//   - mirror-pool   → mirror + up to MaxParallelWorktrees worktrees
//
// Zero git work happens in the package outside of clone, fetch, repack,
// and worktree add/remove. Commits, PRs, merges, and history rewrites
// are operator-skill work that lives inside the worktree Claude session.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jbcom/radioactive-ralph/internal/variant"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
)

// Manager coordinates the per-variant on-disk workspace.
type Manager struct {
	// RepoPath is the absolute path to the operator's repo.
	RepoPath string

	// Paths is the resolved XDG layout for this repo.
	Paths xdg.Paths

	// Variant is the active profile driving the workspace.
	Variant variant.Profile

	// Isolation, ObjectStore, SyncSource, LFS are the resolved values
	// (variant default, possibly overridden by config.toml, with safety
	// floors pinned at config.Resolve time).
	Isolation   variant.IsolationMode
	ObjectStore variant.ObjectStoreMode
	SyncSource  variant.SyncSource
	LFS         variant.LFSMode

	// CopyHooksEnabled controls whether operator's .git/hooks are copied
	// to mirror.git on Init. Default true.
	CopyHooksEnabled bool

	// stateOnce guards lazy init of the worktree pool state.
	stateOnce sync.Once
	state     *worktreeState
}

// New constructs a Manager for repoPath with the given profile and
// resolved knob values. The repoPath MUST be an existing git repo;
// callers verify this in pre-flight.
func New(repoPath string, p variant.Profile, isolation variant.IsolationMode,
	objectStore variant.ObjectStoreMode, syncSource variant.SyncSource,
	lfs variant.LFSMode,
) (*Manager, error) {
	if repoPath == "" {
		return nil, errors.New("workspace: RepoPath required")
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("workspace: abs path %q: %w", repoPath, err)
	}
	paths, err := xdg.Resolve(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolve xdg paths: %w", err)
	}
	return &Manager{
		RepoPath:         abs,
		Paths:            paths,
		Variant:          p,
		Isolation:        isolation,
		ObjectStore:      objectStore,
		SyncSource:       syncSource,
		LFS:              lfs,
		CopyHooksEnabled: true,
	}, nil
}

// Init performs first-run setup for this variant's workspace. Safe to
// call multiple times — each step checks whether the target already
// exists and is correctly formed.
//
// Idempotent actions:
//   - IsolationShared: ensures RepoPath exists, returns.
//   - IsolationShallow: ensures shallow clone exists; fetches if stale.
//   - IsolationMirrorSingle / IsolationMirrorPool: ensures mirror.git
//     exists, fetches from configured remotes, copies hooks, sets LFS
//     config, repacks if corruption detected.
func (m *Manager) Init(ctx context.Context) error {
	switch m.Isolation {
	case variant.IsolationShared:
		return m.initShared()
	case variant.IsolationShallow:
		return m.initShallow(ctx)
	case variant.IsolationMirrorSingle, variant.IsolationMirrorPool:
		return m.initMirror(ctx)
	default:
		return fmt.Errorf("workspace: unknown isolation mode %q", m.Isolation)
	}
}

// initShared validates the operator's repo path. Nothing to clone.
func (m *Manager) initShared() error {
	if err := assertGitRepo(m.RepoPath); err != nil {
		return fmt.Errorf("workspace(shared): %w", err)
	}
	// Shared isolation still writes Ralph-owned state under XDG (event
	// log, sockets, logs). Create those now.
	if err := m.Paths.Ensure(); err != nil {
		return fmt.Errorf("workspace(shared): ensure state dirs: %w", err)
	}
	return nil
}

// initShallow creates a depth-1 clone under Paths.Shallow if missing.
// Re-uses it on subsequent calls; a fresh fetch happens at session
// spawn time via Fetch().
func (m *Manager) initShallow(ctx context.Context) error {
	if err := assertGitRepo(m.RepoPath); err != nil {
		return fmt.Errorf("workspace(shallow): %w", err)
	}
	if err := m.Paths.Ensure(); err != nil {
		return fmt.Errorf("workspace(shallow): ensure state dirs: %w", err)
	}
	if _, err := os.Stat(filepath.Join(m.Paths.Shallow, ".git")); err == nil {
		return nil
	}
	// Clone from local repo with --depth=1 to keep it cheap.
	args := []string{"clone", "--depth=1", fileURL(m.RepoPath), m.Paths.Shallow}
	if err := runGit(ctx, "", args...); err != nil {
		return fmt.Errorf("workspace(shallow): clone: %w", err)
	}
	return m.applyLFSConfig(ctx, m.Paths.Shallow)
}

// initMirror creates the bare mirror clone under Paths.MirrorGit if
// missing, then ensures hooks + LFS config + remotes are configured.
func (m *Manager) initMirror(ctx context.Context) error {
	if err := assertGitRepo(m.RepoPath); err != nil {
		return fmt.Errorf("workspace(mirror): %w", err)
	}
	if err := m.Paths.Ensure(); err != nil {
		return fmt.Errorf("workspace(mirror): ensure state dirs: %w", err)
	}

	if _, err := os.Stat(filepath.Join(m.Paths.MirrorGit, "HEAD")); err != nil {
		// Fresh clone. Object store mode chooses whether to share objects.
		args := []string{"clone", "--mirror"}
		if m.ObjectStore == variant.ObjectStoreReference {
			// --reference shares the operator's object pool. Ralph never
			// runs prune/gc on the mirror — that would invalidate the
			// operator's repo. Omitting --dissociate keeps the mirror
			// borrowing objects (the only mode that makes --reference
			// worth it).
			args = append(args, "--reference", filepath.Join(m.RepoPath, ".git"))
		}
		args = append(args, fileURL(m.RepoPath), m.Paths.MirrorGit)
		if err := runGit(ctx, "", args...); err != nil {
			return fmt.Errorf("workspace(mirror): clone: %w", err)
		}
	}

	if err := m.configureRemotes(ctx); err != nil {
		return fmt.Errorf("workspace(mirror): configure remotes: %w", err)
	}
	if m.CopyHooksEnabled {
		if err := CopyHooks(m.RepoPath, m.Paths.MirrorGit); err != nil {
			return fmt.Errorf("workspace(mirror): copy hooks: %w", err)
		}
	}
	if err := m.applyLFSConfig(ctx, m.Paths.MirrorGit); err != nil {
		return fmt.Errorf("workspace(mirror): lfs config: %w", err)
	}
	return nil
}

// configureRemotes sets up the mirror's remotes according to SyncSource.
//
//   - local: one remote 'local' pointing at file://RepoPath
//   - origin: one remote 'origin' inherited from the clone (unchanged)
//   - both: 'local' added alongside existing 'origin'
func (m *Manager) configureRemotes(ctx context.Context) error {
	switch m.SyncSource {
	case variant.SyncSourceOrigin:
		return nil // clone --mirror already set origin to fileURL(RepoPath)
	case variant.SyncSourceLocal, variant.SyncSourceBoth:
		return m.ensureRemote(ctx, "local", fileURL(m.RepoPath))
	default:
		return fmt.Errorf("unknown sync source %q", m.SyncSource)
	}
}

// ensureRemote adds a named remote if missing, updates it if URL differs.
func (m *Manager) ensureRemote(ctx context.Context, name, url string) error {
	out, err := gitOutput(ctx, m.Paths.MirrorGit, "remote", "get-url", name)
	if err == nil {
		if strings.TrimSpace(out) == url {
			return nil
		}
		return runGit(ctx, m.Paths.MirrorGit, "remote", "set-url", name, url)
	}
	return runGit(ctx, m.Paths.MirrorGit, "remote", "add", name, url)
}

// Fetch refreshes the mirror from its configured remotes. Two-remote
// variants (sync_source=both) fetch from local first (fast, shared
// objects), then from origin to pick up anything merged remotely.
func (m *Manager) Fetch(ctx context.Context) error {
	if m.Isolation == variant.IsolationShared {
		return nil
	}
	target := m.Paths.MirrorGit
	if m.Isolation == variant.IsolationShallow {
		target = m.Paths.Shallow
	}
	switch m.SyncSource {
	case variant.SyncSourceLocal:
		return runGit(ctx, target, "fetch", "local")
	case variant.SyncSourceOrigin:
		return runGit(ctx, target, "fetch", "origin")
	case variant.SyncSourceBoth:
		// Local first for cheap object reuse, then origin.
		if err := runGit(ctx, target, "fetch", "local"); err != nil {
			return fmt.Errorf("fetch local: %w", err)
		}
		return runGit(ctx, target, "fetch", "origin")
	default:
		return fmt.Errorf("unknown sync source %q", m.SyncSource)
	}
}

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
