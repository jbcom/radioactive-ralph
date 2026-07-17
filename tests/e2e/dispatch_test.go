package e2e

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestE2E_DispatchThroughRealSupervisorWithFakeProvider is the
// CI-feasible dispatch flow: a real supervisor process (real IPC socket,
// real reaper tick), a real orch.Orchestrator.DispatchNext call running
// AGAINST THE SAME STORE the supervisor uses, and a REAL subprocess turn
// via internal/provider.ClaudeRunner (which owns the subprocess's pty and
// watchdog exactly as it would for a real hosted model) — but pointed at
// a fake `claude` script on PATH (tests/e2e/fakeprovider.go) instead of a
// real hosted model, so this exercises the full pty-owning,
// watchdog-supervised provider path with zero real agent spend.
//
// There is no CLI surface to trigger a dispatch in this build (see
// flow_test.go's doc comment on cmd/radioactive_ralph's cobra command
// set), so DispatchNext is driven in-process here, directly against the
// on-disk store file the concurrently-running supervisor also has open —
// SQLite's WAL mode is built for exactly this multi-process access (see
// store.DSN's doc comment), so this is a faithful exercise of what a real
// dispatch pipeline would do while the supervisor is up, not a
// workaround.
//
// After DispatchNext runs, the client TUI is driven under a real pty all
// the way to the micro (single-task) view to confirm the dispatched
// task's real completion is visible there.
func TestE2E_DispatchThroughRealSupervisorWithFakeProvider(t *testing.T) {
	bin := BuildBinary(t)
	env := NewIsolatedEnv(t)
	env.MaterializeFixture(t)
	fakeClaudeDir := WriteFakeClaudeCLI(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- start the supervisor. Its own PATH doesn't need the fake CLI
	// (the supervisor never spawns a provider itself in this build; only
	// DispatchNext below does, in-process), but it shares the same
	// isolated env otherwise. ---
	supCmd := exec.CommandContext(ctx, bin, "--supervisor") //nolint:gosec // bin is our own just-built binary
	supCmd.Dir = env.ProjectDir
	supCmd.Env = env.Environ()
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
	})

	waitForSupervisorSocket(t, env)

	if out, err := RunOnce(context.Background(), t, bin, env, "--init"); err != nil {
		t.Fatalf("--init: %v\n%s", err, out)
	}

	// --- seed a plan/task and dispatch it via a REAL orch.Orchestrator
	// pointed at the fake claude CLI, in-process, against the same store
	// file the supervisor has open. ---
	dbPath := filepath.Join(env.StateDir, "ralph.db")
	st, err := store.Open(context.Background(), store.Options{DSN: store.DSN(dbPath)})
	if err != nil {
		t.Fatalf("open shared store: %v", err)
	}
	defer func() { _ = st.Close() }()

	resolvedProjectDir := env.ProjectDir
	if resolved, err := filepath.EvalSymlinks(env.ProjectDir); err == nil {
		resolvedProjectDir = resolved
	}
	fps, err := store.Fingerprints(context.Background(), resolvedProjectDir)
	if err != nil {
		t.Fatalf("fingerprints: %v", err)
	}
	projectID, found, err := st.ResolveProject(context.Background(), fps)
	if err != nil || !found {
		t.Fatalf("resolve project: found=%v err=%v", found, err)
	}

	planTitle := "Dispatch the E2E widget"
	taskDesc := "wire up the frobnicator via the fake CLI"
	planID, err := st.CreatePlan(context.Background(), store.CreatePlanOpts{
		ProjectID:      projectID,
		Slug:           "e2e-dispatch-plan",
		Title:          planTitle,
		SourceMarkdown: "# " + planTitle + "\n\n1. " + taskDesc + "\n",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := st.SetPlanStatus(context.Background(), planID, store.PlanStatusActive); err != nil {
		t.Fatalf("SetPlanStatus: %v", err)
	}
	// No manual CreateTask here: DispatchNext/claimStepTask materializes
	// the task itself from the parsed markdown's step ID (a dotted-index
	// string like "0", not an arbitrary caller-chosen ID) the first time
	// it dispatches that step.

	fakeClaudeBin := filepath.Join(fakeClaudeDir, "claude")
	o := orch.New(st,
		orch.WithBindingResolver(func(_ context.Context, _ string, _ bool) (provider.Binding, error) {
			return provider.Binding{
				Name:   "claude",
				Config: provider.BindingConfig{Type: "claude", Binary: fakeClaudeBin},
			}, nil
		}),
	)

	dispatchCtx, dispatchCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer dispatchCancel()
	dispatched, err := o.DispatchNext(dispatchCtx, projectID, planID)
	if err != nil {
		t.Fatalf("DispatchNext: %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("DispatchNext dispatched = %d, want 1", dispatched)
	}

	// DispatchNext/claimStepTask materializes the task under the plan's
	// own step-ID scheme (StepRef.ID(), a dot-joined GroupPath+Index) —
	// a single top-level ordered-list item under the doc's sole heading
	// group is step "0.0", not a caller-chosen ID.
	const taskID = "0.0"
	task, err := st.GetTask(context.Background(), planID, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != store.TaskStatusDone {
		t.Fatalf("task status = %q, want done (VerifyAndComplete should have accepted the fake CLI's evidence)", task.Status)
	}

	// --- drive the client TUI under a real pty all the way to micro,
	// confirming the real dispatch's completion is visible there. ---
	proc := StartPTY(t, bin, env)
	defer proc.Close()

	proc.Expect(planTitle, 10*time.Second)
	proc.Send(KeyEnter)
	proc.Expect(taskDesc, 10*time.Second)
	proc.ExpectFunc("task shows done status", 10*time.Second, func(b []byte) bool {
		return bytes.Contains(b, []byte("done"))
	})

	proc.Send(KeyEnter) // drill meso -> micro
	proc.Expect("task: "+taskID, 10*time.Second)

	proc.Send(KeyLeft) // micro -> meso
	proc.Expect("drill into task", 10*time.Second)
	proc.Send(KeyLeft) // meso -> macro
	proc.Expect("drill into plan", 10*time.Second)

	proc.Send(KeyQuit)
	if err := proc.Wait(10 * time.Second); err != nil {
		t.Fatalf("client TUI did not exit cleanly: %v", err)
	}
}
