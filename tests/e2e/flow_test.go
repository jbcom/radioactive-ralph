// Package e2e's flow_test.go is the CI-feasible Layer-2 E2E flow (Phase
// 8): a real supervisor process, a real --init against a fixture project
// dir, a real (non-tty) client status check, and a real client TUI driven
// under a pty with real keystrokes — proving the whole path end-to-end
// with actual OS processes, not just the in-process teatest model drive
// Layer 1 already covers.
package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestE2E_SupervisorInitClientTUIFlow is the full CI-feasible real-binary
// flow: start --supervisor, --init a fixture project, confirm the plain
// client sees it, seed a plan directly into the shared store (there is no
// CLI surface to create/dispatch a plan in this build — see
// cmd/radioactive_ralph's cobra command set), then drive the client TUI
// under a real pty: WaitFor the plan title at macro, drill into meso,
// WaitFor the task, drill back out, and quit cleanly.
func TestE2E_SupervisorInitClientTUIFlow(t *testing.T) {
	bin := BuildBinary(t)
	env := NewIsolatedEnv(t)
	env.MaterializeFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- start the supervisor in the background. ---
	supCmd := exec.CommandContext(ctx, bin, "--supervisor") //nolint:gosec // bin is our own just-built binary
	supCmd.Dir = env.ProjectDir
	supCmd.Env = env.Environ()
	var supOut strings.Builder
	supCmd.Stdout = &supOut
	supCmd.Stderr = &supOut
	if err := supCmd.Start(); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	supDone := make(chan error, 1)
	go func() { supDone <- supCmd.Wait() }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-supDone:
		case <-time.After(5 * time.Second):
			_ = supCmd.Process.Kill()
		}
		if t.Failed() {
			t.Logf("supervisor output:\n%s", supOut.String())
		}
	})

	waitForSupervisorSocket(t, env)

	// --- --init the fixture project. ---
	initOut, err := RunOnce(context.Background(), t, bin, env, "--init")
	if err != nil {
		t.Fatalf("--init: %v\n%s", err, initOut)
	}
	if !strings.Contains(initOut, "initialized") {
		t.Fatalf("--init output did not confirm initialization:\n%s", initOut)
	}

	// --- plain client, non-tty: must report supervisor status. ---
	statusOut, err := RunOnce(context.Background(), t, bin, env)
	if err != nil {
		t.Fatalf("client status: %v\n%s", err, statusOut)
	}
	if !strings.Contains(statusOut, "supervisor is up") {
		t.Fatalf("client status output unexpected:\n%s", statusOut)
	}

	// --- seed a plan directly into the shared store. ---
	planTitle := "Ship the E2E widget"
	taskDesc := "wire up the frobnicator"
	seedPlan(t, env, planTitle, taskDesc)

	// --- drive the client TUI under a real pty. ---
	proc := StartPTY(t, bin, env)
	defer proc.Close()

	// The TUI paints into the alt-screen; per Phase 7's manual
	// verification notes, allow a real settle window rather than a fixed
	// short sleep, and poll for the seeded plan title.
	proc.Expect(planTitle, 10*time.Second)

	// Drill in: macro -> meso.
	proc.Send(KeyEnter)
	proc.Expect(taskDesc, 10*time.Second)

	// Drill out: meso -> macro. Uses the left-arrow key rather than a
	// bare Esc: the model's handleKey treats esc/backspace/left/h as
	// equivalent drill-out keys, and a lone 0x1b (Esc) byte carries a
	// real terminal-input ambiguity — Bubble Tea's reader must briefly
	// wait to see whether it's a bare Esc keypress or the first byte of
	// a CSI sequence — that a CSI-prefixed arrow key does not. The
	// macro-only footer text ("drill into plan") is unambiguous proof of
	// the level change: the plan title alone also appears in the meso
	// header, so asserting on it here would pass even if the keystroke
	// were dropped.
	proc.Send(KeyLeft)
	proc.Expect("drill into plan", 10*time.Second)

	// Quit cleanly.
	proc.Send(KeyQuit)
	if err := proc.Wait(10 * time.Second); err != nil {
		t.Fatalf("client TUI did not exit cleanly: %v", err)
	}
}

// waitForSupervisorSocket polls until the supervisor's status endpoint is
// reachable via the plain client, bounded so a supervisor that never
// comes up fails the test instead of hanging it.
func waitForSupervisorSocket(t *testing.T, env *IsolatedEnv) {
	t.Helper()
	socketPath := filepath.Join(env.StateDir, "service.sock")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("supervisor socket %s never appeared within 10s", socketPath)
}

// seedPlan opens the shared store the running supervisor/client both use
// (same DSN, same file) and writes a plan + one task directly — the
// mechanism a real dispatch pipeline would otherwise populate, standing in
// here because this build's CLI has no plan-create/enqueue subcommand.
func seedPlan(t *testing.T, env *IsolatedEnv, planTitle, taskDesc string) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(env.StateDir, "ralph.db")
	st, err := store.Open(ctx, store.Options{DSN: store.DSN(dbPath)})
	if err != nil {
		t.Fatalf("open shared store to seed plan: %v", err)
	}
	defer func() { _ = st.Close() }()

	// Resolve symlinks the same way the child process's own os.Getwd()
	// implicitly does after chdir (e.g. macOS /tmp -> /private/tmp): the
	// running --init invocation fingerprinted its resolved cwd, so
	// re-fingerprinting the raw, possibly-symlinked env.ProjectDir here
	// would compute a different abs_path and never find the project.
	resolvedProjectDir := env.ProjectDir
	if resolved, err := filepath.EvalSymlinks(env.ProjectDir); err == nil {
		resolvedProjectDir = resolved
	}
	fps, err := store.Fingerprints(ctx, resolvedProjectDir)
	if err != nil {
		t.Fatalf("compute fixture project fingerprints: %v", err)
	}
	projectID, found, err := st.ResolveProject(ctx, fps)
	if err != nil {
		t.Fatalf("resolve fixture project (after --init): %v", err)
	}
	if !found {
		t.Fatal("fixture project not found in store after --init")
	}

	planID, err := st.CreatePlan(ctx, store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "e2e-plan",
		Title:          planTitle,
		SourceMarkdown: "# " + planTitle + "\n\n1. " + taskDesc + "\n",
	})
	if err != nil {
		t.Fatalf("seed CreatePlan: %v", err)
	}
	// CreatePlan always inserts status=draft; the TUI macro view's
	// ListPlans (like a real operator's dashboard) only shows
	// active/paused plans by default, so a freshly seeded plan must be
	// activated to be visible — exactly what a real dispatch pipeline
	// would do before starting work on it.
	if err := st.SetPlanStatus(ctx, planID, store.PlanStatusActive); err != nil {
		t.Fatalf("seed SetPlanStatus: %v", err)
	}
	if err := st.CreateTask(ctx, store.CreateTaskOpts{
		PlanID:      planID,
		ID:          "task-a",
		Description: taskDesc,
	}); err != nil {
		t.Fatalf("seed CreateTask: %v", err)
	}
}
