// Package integration_test runs always-on end-to-end checks that
// exercise the radioactive_ralph binary against real fixtures (tmp git
// repos, real unix sockets). Authentication-gated tests live in
// cassette_test.go and live_test.go.
package integration_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// buildRalph compiles cmd/radioactive_ralph into a tempfile and
// returns its path. Uses a module-scoped sync.Once so repeat builds
// across sub-tests stay fast.
func buildRalph(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "radioactive_ralph"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", bin,
		"github.com/jbcom/radioactive-ralph/cmd/radioactive_ralph")
	cmd.Dir = projectRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build radioactive_ralph: %v\n%s", err, out)
	}
	return bin
}

// projectRoot returns the repo root by walking up from CWD until it
// finds go.mod.
func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from CWD")
		}
		dir = parent
	}
}

// newGitRepo creates a real tmp repo with one commit on main.
func newGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustGit(t, dir, "init", "-q", "-b", "main")
	mustGit(t, dir, "config", "user.email", "ralph@example.com")
	mustGit(t, dir, "config", "user.name", "Ralph")
	mustGit(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustGit(t, dir, "add", "README.md")
	mustGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func mustGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// shortTempDir returns a short /tmp/ path to avoid the macOS
// 104-byte Unix socket limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	base := os.TempDir()
	if runtime.GOOS == "darwin" {
		base = "/tmp"
	}
	d, err := os.MkdirTemp(base, "itest-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func seedActivePlan(t *testing.T, bin, repo string, env []string) {
	t.Helper()
	planPath := filepath.Join(repo, "plan-bootstrap.json")
	body := `{
  "slug": "itest",
  "title": "integration test plan",
  "primary_variant": "blue",
  "confidence": 100,
  "tasks": [
    {
      "id": "inspect-state",
      "description": "Inspect repo service state",
      "complexity": "S",
      "effort": "S",
      "variant_hint": "blue"
    }
  ]
}`
	if err := os.WriteFile(planPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write bootstrap plan: %v", err)
	}

	cmd := exec.Command(bin, "plan", "import", planPath)
	cmd.Dir = repo
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("plan import: %v\n%s", err, out)
	}
}

// ── Always-on: init round-trip -----------------------------------

func TestRalphInitCreatesScaffold(t *testing.T) {
	bin := buildRalph(t)
	repo := newGitRepo(t)

	cmd := exec.Command(bin, "init", "--yes")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ralph init: %v\n%s", err, out)
	}

	for _, want := range []string{
		filepath.Join(repo, ".radioactive-ralph", "config.toml"),
		filepath.Join(repo, ".radioactive-ralph", "local.toml"),
		filepath.Join(repo, ".radioactive-ralph", "plans", "index.md"),
		filepath.Join(repo, ".gitignore"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("expected %s to exist: %v", want, err)
		}
	}
}

// ── Always-on: full repo-service lifecycle -----------------------

func TestRalphRunStatusStopRoundTrip(t *testing.T) {
	bin := buildRalph(t)
	repo := newGitRepo(t)

	// Isolate state dir so the test doesn't collide with the dev
	// laptop's real ~/Library state.
	stateDir := shortTempDir(t)
	env := append(os.Environ(),
		"RALPH_STATE_DIR="+stateDir,
		"RALPH_SERVICE_CONTEXT=", // clear any inherited flag
	)

	// Bootstrap: init, then seed a real active plan via plan import so
	// the repo satisfies the plans-first gate honestly.
	initCmd := exec.Command(bin, "init", "--yes")
	initCmd.Dir = repo
	initCmd.Env = env
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	seedActivePlan(t, bin, repo, env)

	// Start the durable repo service in the background.
	runCmd := exec.Command(bin, "service", "start", "--repo-root", repo)
	runCmd.Dir = repo
	runCmd.Env = env
	var runOut strings.Builder
	runCmd.Stdout = &runOut
	runCmd.Stderr = &runOut
	if err := runCmd.Start(); err != nil {
		t.Fatalf("run start: %v", err)
	}
	defer func() {
		_ = runCmd.Process.Kill()
		_ = runCmd.Wait()
	}()

	// Poll for a live status response.
	deadline := time.Now().Add(10 * time.Second)
	var statusOK bool
	var statusLastErr string
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		statusCmd := exec.Command(bin, "status", "--repo-root", repo, "--json")
		statusCmd.Env = env
		out, err := statusCmd.CombinedOutput()
		if err == nil {
			// Parse the JSON status body.
			var status map[string]any
			if err := json.Unmarshal(out, &status); err == nil {
				if v, ok := status["repo_path"].(string); ok && v != "" {
					statusOK = true
					break
				}
			}
		}
		statusLastErr = string(out)
	}
	if !statusOK {
		t.Fatalf("status never succeeded within 10s; last output: %s\nservice log:\n%s",
			statusLastErr, runOut.String())
	}

	// Stop the repo service.
	stopCmd := exec.Command(bin, "stop", "--repo-root", repo)
	stopCmd.Env = env
	if out, err := stopCmd.CombinedOutput(); err != nil {
		t.Fatalf("stop: %v\n%s", err, out)
	}

	// Wait for run to exit cleanly.
	done := make(chan error, 1)
	go func() { done <- runCmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("run exited with error: %v\nlog:\n%s", err, runOut.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("repo service did not exit within 5s of stop command")
	}
}

// ── Always-on: plans-first discipline ----------------------------

func TestRalphRunRefusesWithoutActivePlan(t *testing.T) {
	bin := buildRalph(t)
	repo := newGitRepo(t)
	stateDir := shortTempDir(t)

	// Deliberately skip `radioactive_ralph init` so no active plan exists.
	// Non-fixit variants must refuse to run.
	cmd := exec.Command(bin, "run", "--variant", "blue")
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "RALPH_STATE_DIR="+stateDir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-fixit run without an active plan to fail; output:\n%s", out)
	}
	if !strings.Contains(string(out), "plans-first discipline") {
		t.Errorf("expected plans-first refusal message, got:\n%s", out)
	}
}

// ── Always-on: --version / --help sanity -------------------------

func TestRalphVersion(t *testing.T) {
	bin := buildRalph(t)
	cmd := exec.Command(bin, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--version: %v\n%s", err, out)
	}
	// "dev (none, built unknown)" in test builds.
	if len(strings.TrimSpace(string(out))) == 0 {
		t.Error("--version produced empty output")
	}
}

func TestRalphHelpListsAllSubcommands(t *testing.T) {
	bin := buildRalph(t)
	cmd := exec.Command(bin, "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--help: %v\n%s", err, out)
	}
	for _, cmdName := range []string{
		"init", "run", "status", "attach", "stop", "doctor", "service",
	} {
		if !strings.Contains(string(out), cmdName) {
			t.Errorf("--help missing subcommand %q:\n%s", cmdName, out)
		}
	}
}
