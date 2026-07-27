package orch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/a2a"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// escapedOutputPlan declares an in-project output. The escape is created AFTER
// admission, which is the whole point: the pre-dispatch check cannot see it.
const escapedOutputPlan = "# Escape after admission\n\n" +
	"1. write the artifact\n\n" +
	"   ```ralph-task\n" +
	`   {"id": "writer", "outputs": [{"path": "build/out.txt"}]}` + "\n" +
	"   ```\n"

// TestCompletionDetectsAnOutputThatEscapedAfterAdmission is the guarantee the
// design doc actually claims. Pre-dispatch containment is best-effort: the
// provider is a separate process writing by pathname minutes later, so a peer
// can replace a directory component after the check returns. What Ralph CAN
// promise is DETECTION — a task whose declared output no longer resolves inside
// the project must not be marked done.
//
// Without a completion-time check that promise was unbacked: nothing called any
// output verification, so an escaped write completed normally.
func TestCompletionDetectsAnOutputThatEscapedAfterAdmission(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "escape-after-admission")
	projectDir := mustProjectDir(t, s, projectID)

	o := New(s, WithBindingResolver(fakeBindingResolver("codex", false)))
	planID := mustImportPlan(t, o, projectID, "escape", escapedOutputPlan)

	// Admission-time state: build/ is a real in-project directory.
	if err := os.MkdirAll(filepath.Join(projectDir, "build"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimTask(ctx, planID, "writer", sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	// A peer replaces build/ with a symlink out of the project — exactly the
	// race the pre-dispatch check cannot close.
	outside := t.TempDir()
	if err := os.RemoveAll(filepath.Join(projectDir, "build")); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(projectDir, "build")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "out.txt"), []byte("escaped"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	done, err := o.VerifyAndComplete(ctx, planID, "writer", a2a.Evidence{Output: "wrote the artifact"})
	if err != nil {
		t.Fatalf("VerifyAndComplete: %v", err)
	}
	if done {
		t.Fatal("a task whose declared output resolved OUTSIDE the project was " +
			"marked done; detection at completion is the guarantee the design " +
			"doc makes, and it was unbacked")
	}

	task, err := s.GetTask(ctx, planID, "writer")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status == store.TaskStatusDone {
		t.Fatalf("task status = %q, want anything but done", task.Status)
	}
}

// TestCompletionAcceptsAContainedOutput is the control. Verification must not
// refuse an honest task: a declared output that stayed inside the project has
// to complete normally, or every annotated plan would stall.
func TestCompletionAcceptsAContainedOutput(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	projectID := mustCreateTestProject(t, s, "contained-completion")
	projectDir := mustProjectDir(t, s, projectID)

	o := New(s, WithBindingResolver(fakeBindingResolver("codex", false)))
	planID := mustImportPlan(t, o, projectID, "contained", escapedOutputPlan)

	if err := os.MkdirAll(filepath.Join(projectDir, "build"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "build", "out.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimTask(ctx, planID, "writer", sessionID, workerID); err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}

	done, err := o.VerifyAndComplete(ctx, planID, "writer", a2a.Evidence{Output: "wrote the artifact"})
	if err != nil {
		t.Fatalf("VerifyAndComplete: %v", err)
	}
	if !done {
		t.Fatal("a task whose declared output stayed inside the project was refused")
	}
}

// mustProjectDir returns the project's recorded checkout directory.
func mustProjectDir(t *testing.T, s *store.Store, projectID string) string {
	t.Helper()
	dir, found, err := s.ProjectAbsPath(context.Background(), projectID)
	if err != nil || !found {
		t.Fatalf("ProjectAbsPath: found=%v err=%v", found, err)
	}
	return dir
}
