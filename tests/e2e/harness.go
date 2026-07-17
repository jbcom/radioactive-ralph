// Package e2e is the Layer-2 real-binary E2E harness (Phase 8): it builds
// the actual radioactive_ralph binary and drives it exactly like an
// operator would — a real supervisor process, a real --init invocation
// against a fixture project directory, and a real client TUI spawned
// under a pty, driven with real keystrokes. This is deliberately distinct
// from internal/tui's teatest-based Layer 1 (which drives the tea.Model
// directly, no real process, no real terminal): Layer 2 exists to catch
// exactly the class of bug Layer 1 structurally cannot — process
// wiring, socket discovery, isatty/pty interaction, and the alt-screen
// content actually painting.
//
// Every helper here isolates the driven binary from the operator's real
// state: RALPH_STATE_DIR always points at a fresh t.TempDir(), so no test
// run ever touches ~/Library/Application Support/radioactive-ralph or
// equivalent.
package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// buildOnce ensures the binary is compiled exactly once per test process,
// even though multiple tests in this package each want a path to it —
// `go build` for this size of binary is a few seconds, and every test
// spawning its own build would multiply that unnecessarily.
var (
	buildOnce sync.Once
	buildPath string
	buildErr  error
)

// BuildBinary compiles ./cmd/radioactive_ralph to a temp path shared for
// the lifetime of the test process and returns its path. Safe to call
// from multiple tests/subtests; the actual `go build` runs at most once.
func BuildBinary(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "radioactive-ralph-e2e-bin-")
		if err != nil {
			buildErr = fmt.Errorf("mkdtemp: %w", err)
			return
		}
		name := "radioactive_ralph"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		out := filepath.Join(dir, name)

		repoRoot, err := repoRootDir()
		if err != nil {
			buildErr = err
			return
		}

		cmd := exec.Command("go", "build", "-o", out, "./cmd/radioactive_ralph") //nolint:gosec // fixed argv, test-only
		cmd.Dir = repoRoot
		var combined []byte
		combined, buildErr = cmd.CombinedOutput()
		if buildErr != nil {
			buildErr = fmt.Errorf("go build ./cmd/radioactive_ralph: %w\n%s", buildErr, combined)
			return
		}
		buildPath = out
	})
	if buildErr != nil {
		t.Fatalf("BuildBinary: %v", buildErr)
	}
	return buildPath
}

// repoRootDir walks up from this source file's directory to the repo
// root (identified by go.mod), so `go build` runs with the right module
// context regardless of the test binary's working directory.
func repoRootDir() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("e2e: could not determine caller info for repo root resolution")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("e2e: go.mod not found walking up from %s", thisFile)
		}
		dir = parent
	}
}

// IsolatedEnv is the isolated environment a driven radioactive_ralph
// process runs under, so it never touches the operator's real state.
type IsolatedEnv struct {
	// StateDir is RALPH_STATE_DIR — the XDG-equivalent state root this
	// process's supervisor/client/store will use.
	StateDir string
	// XDGDataHome and XDGRuntimeDir are set alongside RALPH_STATE_DIR so
	// any code path that falls back to raw XDG vars (rather than the
	// RALPH_STATE_DIR override) still lands in the same isolated tree.
	XDGDataHome   string
	XDGRuntimeDir string
	// ProjectDir is the fixture project directory this invocation's
	// client/--init commands operate against (their process cwd).
	ProjectDir string
	// Home is an isolated HOME so anything consulting os.UserHomeDir
	// (service install paths, git config resolution) never touches the
	// operator's real home directory either.
	Home string
}

// NewIsolatedEnv allocates a fresh isolated environment rooted at
// t.TempDir(): a state dir, an XDG data/runtime pair, an isolated HOME,
// and (if seedProject is true) a fixture project directory materialized
// from the reference test-repo.
func NewIsolatedEnv(t *testing.T) *IsolatedEnv {
	t.Helper()
	root := t.TempDir()
	env := &IsolatedEnv{
		StateDir:      filepath.Join(root, "state"),
		XDGDataHome:   filepath.Join(root, "xdg-data"),
		XDGRuntimeDir: filepath.Join(root, "xdg-runtime"),
		ProjectDir:    filepath.Join(root, "project"),
		Home:          filepath.Join(root, "home"),
	}
	for _, dir := range []string{env.StateDir, env.XDGDataHome, env.XDGRuntimeDir, env.ProjectDir, env.Home} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	return env
}

// Environ returns the os/exec-ready environment slice for a process
// running under this isolated env: a minimal PATH-preserving base plus
// the isolation vars, so the driven binary can still find `git` (needed
// by store.Fingerprints' isGitRepo check) without inheriting the rest of
// the operator's real environment/state.
func (e *IsolatedEnv) Environ() []string {
	base := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + e.Home,
		"RALPH_STATE_DIR=" + e.StateDir,
		"XDG_DATA_HOME=" + e.XDGDataHome,
		"XDG_RUNTIME_DIR=" + e.XDGRuntimeDir,
	}
	// TERM matters for the pty-driven client: Bubble Tea/lipgloss probe
	// it to decide the color profile, and an unset TERM under a
	// non-interactive test runner can make that detection misbehave.
	if term := os.Getenv("TERM"); term != "" {
		base = append(base, "TERM="+term)
	} else {
		base = append(base, "TERM=xterm-256color")
	}
	return base
}

// MaterializeFixture copies the reference test-repo fixture
// (~/src/reference-codebases/test-repo) into ProjectDir, minus its own
// .git directory (a fresh `git init` below gives the fixture project its
// own independent identity so cloning the same source fixture twice in
// parallel subtests never collide on a shared .git). store.Fingerprints
// only needs `git rev-parse`/`git remote` to succeed against a repo that
// exists — an empty freshly-initialized repo satisfies that.
func (e *IsolatedEnv) MaterializeFixture(t *testing.T) {
	t.Helper()
	src := fixtureSourceDir(t)

	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read fixture dir %s: %v", src, err)
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		if err := copyTree(filepath.Join(src, entry.Name()), filepath.Join(e.ProjectDir, entry.Name())); err != nil {
			t.Fatalf("copy fixture entry %s: %v", entry.Name(), err)
		}
	}

	initCmd := exec.Command("git", "init") //nolint:gosec // fixed argv, test-only
	initCmd.Dir = e.ProjectDir
	initCmd.Env = e.Environ()
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("git init fixture project: %v\n%s", err, out)
	}
}

// fixtureSourceDir resolves ~/src/reference-codebases/test-repo, skipping
// the test with a clear reason if it is not present on this machine (the
// fixture is a local reference clone, not a repo-tracked asset).
func fixtureSourceDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve real home dir for fixture lookup: %v", err)
	}
	src := filepath.Join(home, "src", "reference-codebases", "test-repo")
	info, err := os.Stat(src)
	if err != nil || !info.IsDir() {
		t.Skipf("fixture source %s not present; skipping (see CLAUDE.md fixtures note)", src)
	}
	return src
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, 0o755); err != nil { //nolint:gosec // test fixture tree, not sensitive
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(src) //nolint:gosec // test fixture tree, not sensitive
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode()) //nolint:gosec // test fixture tree
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in) //nolint:gosec // bounded test fixture size
	return err
}

// RunOnce runs binPath with args under env, waiting for it to exit, and
// returns its combined stdout+stderr. Used for short-lived invocations
// (--init, the non-tty client status print) that are not expected to
// block.
func RunOnce(ctx context.Context, t *testing.T, binPath string, env *IsolatedEnv, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(ctx, binPath, args...) //nolint:gosec // binPath is our own just-built binary
	cmd.Dir = env.ProjectDir
	cmd.Env = env.Environ()
	out, err := cmd.CombinedOutput()
	return string(out), err
}
