// Package xdg resolves Ralph's state directory and per-repo workspace paths.
//
// State lives at one of these, in priority order:
//
//  1. $RALPH_STATE_DIR (explicit override, for tests)
//  2. $XDG_STATE_HOME/radioactive-ralph (Linux, WSL)
//  3. ~/Library/Application Support/radioactive-ralph (macOS)
//  4. %LOCALAPPDATA%/radioactive-ralph (Windows; only a few subcommands
//     are supported on Windows natively, the full daemon expects POSIX)
//  5. ~/.local/state/radioactive-ralph (POSIX default when XDG unset)
//
// Within that root, every per-repo workspace is keyed by a stable hash of
// the absolute path of the operator's repo. Cloning the same repo to two
// locations yields two independent workspaces, which is the correct
// behavior because Ralph's worktrees and event log are tied to the source
// tree on disk, not to the remote URL.
package xdg

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// AppName is the directory component used under every state root.
const AppName = "radioactive-ralph"

// RepoHashLen is the number of hex characters of the repo hash we retain.
// Sixteen gives us a 64-bit keyspace which is plenty for per-machine
// collision avoidance and keeps paths readable.
const RepoHashLen = 16

// ErrRepoPathRequired is returned when a caller passes an empty repo path.
var ErrRepoPathRequired = errors.New("xdg: repo path is required")

// Paths holds the resolved set of directories for a single repo workspace.
// All fields are absolute paths; no field is created on disk until a caller
// asks (Paths is a plan, not a mkdir).
type Paths struct {
	// StateRoot is the root of all Ralph workspaces on this machine, e.g.
	// ~/.local/state/radioactive-ralph or
	// ~/Library/Application Support/radioactive-ralph.
	StateRoot string

	// Workspace is StateRoot/<repo-hash>/ — the per-repo tree.
	Workspace string

	// MirrorGit is Workspace/mirror.git — the bare mirror clone.
	MirrorGit string

	// Shallow is Workspace/shallow — shallow-clone checkout for variants
	// that use shallow isolation.
	Shallow string

	// Worktrees is Workspace/worktrees — parent dir for per-variant worktrees.
	Worktrees string

	// Sessions is Workspace/sessions — per-variant socket/PID/log/alive files.
	Sessions string

	// Logs is Workspace/logs — per-variant rolling log files.
	Logs string

	// StateDB is Workspace/state.db — the SQLite event log path.
	StateDB string

	// Inventory is Workspace/inventory.json — capability discovery snapshot.
	Inventory string
}

// Resolve returns the full Paths plan for the given absolute repo path.
//
// The repo path is converted to its absolute, symlink-resolved form before
// hashing so that ~/work and /Users/me/work produce the same hash.
func Resolve(repoPath string) (Paths, error) {
	var zero Paths
	if repoPath == "" {
		return zero, ErrRepoPathRequired
	}

	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return zero, fmt.Errorf("xdg: resolve abs path for %q: %w", repoPath, err)
	}

	// EvalSymlinks is best-effort — if the path doesn't exist yet we still
	// want to produce a stable hash rather than fail. This matters during
	// `radioactive_ralph init` where the workspace dir hasn't been created yet.
	if evaluated, err := filepath.EvalSymlinks(abs); err == nil {
		abs = evaluated
	}

	root, err := stateRoot()
	if err != nil {
		return zero, err
	}

	workspace := filepath.Join(root, repoHash(abs))
	return Paths{
		StateRoot: root,
		Workspace: workspace,
		MirrorGit: filepath.Join(workspace, "mirror.git"),
		Shallow:   filepath.Join(workspace, "shallow"),
		Worktrees: filepath.Join(workspace, "worktrees"),
		Sessions:  filepath.Join(workspace, "sessions"),
		Logs:      filepath.Join(workspace, "logs"),
		StateDB:   filepath.Join(workspace, "state.db"),
		Inventory: filepath.Join(workspace, "inventory.json"),
	}, nil
}

// repoHash returns the first RepoHashLen hex chars of sha256(absPath).
func repoHash(absPath string) string {
	sum := sha256.Sum256([]byte(absPath))
	return hex.EncodeToString(sum[:])[:RepoHashLen]
}

// stateRoot returns the absolute path to the Ralph state root for this
// machine and user, respecting overrides.
// StateRoot returns the machine-wide state directory for Ralph,
// honoring the $RALPH_STATE_DIR override for tests.
//
// Exported so packages outside the xdg package (the plan subcommand,
// the MCP server bootstrap, etc.) can land the plandag SQLite file
// and other global artifacts under the same root as per-repo
// workspaces.
func StateRoot() (string, error) {
	return stateRoot()
}

func stateRoot() (string, error) {
	if override := os.Getenv("RALPH_STATE_DIR"); override != "" {
		return filepath.Clean(override), nil
	}

	// Linux & WSL honor XDG_STATE_HOME first.
	if runtime.GOOS == "linux" {
		if x := os.Getenv("XDG_STATE_HOME"); x != "" {
			return filepath.Join(x, AppName), nil
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("xdg: resolve home dir: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", AppName), nil
	case "windows":
		if appData := os.Getenv("LOCALAPPDATA"); appData != "" {
			return filepath.Join(appData, AppName), nil
		}
		return filepath.Join(home, "AppData", "Local", AppName), nil
	default:
		// Linux, WSL, other POSIX. XDG_STATE_HOME default is ~/.local/state.
		return filepath.Join(home, ".local", "state", AppName), nil
	}
}

// Ensure creates the workspace subdirectories on disk with 0o700 mode.
//
// Called by `radioactive_ralph init` after the pre-flight wizard. Idempotent — safe to
// run multiple times. Does NOT create mirror.git or state.db; those are
// the responsibility of the workspace manager and the db package.
func (p Paths) Ensure() error {
	dirs := []string{p.Workspace, p.Worktrees, p.Sessions, p.Logs}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("xdg: ensure %q: %w", dir, err)
		}
	}
	return nil
}
