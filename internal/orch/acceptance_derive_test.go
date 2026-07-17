package orch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/a2a"
	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// TestDefaultAcceptanceJSONDerivesCommand proves an inline `accept:` marker
// in a plan step becomes a real, re-runnable acceptance command — the change
// that closes the "any non-empty worker output passes verification" gap.
func TestDefaultAcceptanceJSONDerivesCommand(t *testing.T) {
	got, err := defaultAcceptanceJSON(plan.Step{
		Text: "run the tests `accept: go test ./...`",
	})
	if err != nil {
		t.Fatalf("defaultAcceptanceJSON: %v", err)
	}
	var acc Acceptance
	if err := json.Unmarshal([]byte(got), &acc); err != nil {
		t.Fatalf("unmarshal derived acceptance %q: %v", got, err)
	}
	if acc.Command != "go test ./..." {
		t.Errorf("derived command = %q, want %q", acc.Command, "go test ./...")
	}
}

// TestDefaultAcceptanceJSONDerivesFileFromDetail proves `accept-file:` is
// picked up from the step's detail paragraph too, not just its text line.
func TestDefaultAcceptanceJSONDerivesFileFromDetail(t *testing.T) {
	got, err := defaultAcceptanceJSON(plan.Step{
		Text:   "produce the artifact",
		Detail: "The build must emit `accept-file: dist/app`",
	})
	if err != nil {
		t.Fatalf("defaultAcceptanceJSON: %v", err)
	}
	var acc Acceptance
	if err := json.Unmarshal([]byte(got), &acc); err != nil {
		t.Fatalf("unmarshal derived acceptance %q: %v", got, err)
	}
	if acc.FileExists != "dist/app" {
		t.Errorf("derived file_exists = %q, want %q", acc.FileExists, "dist/app")
	}
}

// TestDefaultAcceptanceJSONNoAnnotationIsEmpty proves a step with no marker
// stays judgment-only (empty acceptance), preserving backward-compatible
// behavior for plans that don't opt into mechanical checks.
func TestDefaultAcceptanceJSONNoAnnotationIsEmpty(t *testing.T) {
	got, err := defaultAcceptanceJSON(plan.Step{Text: "just do the thing"})
	if err != nil {
		t.Fatalf("defaultAcceptanceJSON: %v", err)
	}
	if got != "" {
		t.Errorf("acceptance = %q, want empty for an unannotated step", got)
	}
}

// TestVerifyRunsAcceptanceInProjectDir proves VerifyAndComplete re-runs the
// acceptance command in the PROJECT'S checkout (its recorded abs_path), not
// the process cwd — the fix for workers/verification launching in the
// supervisor's irrelevant working directory. The acceptance command probes
// for a sentinel file that exists only in the project dir, so a check run in
// the wrong directory would fail.
func TestVerifyRunsAcceptanceInProjectDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell script — skip on windows")
	}
	ctx := context.Background()
	s := newTestStore(t)
	o := New(s)

	projectID := mustCreateTestProject(t, s, "verify-dir-project")
	projectDir, found, err := s.ProjectAbsPath(ctx, projectID)
	if err != nil || !found {
		t.Fatalf("ProjectAbsPath: found=%v err=%v", found, err)
	}
	// Drop a sentinel only in the project dir; the acceptance command below
	// passes iff it runs there.
	if err := os.WriteFile(filepath.Join(projectDir, "sentinel.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	planID := mustCreateTestPlan(t, s, projectID, "verify-dir-plan", "Ship", "# Ship\n\n- do it\n")
	if err := s.CreateTask(ctx, store.CreateTaskOpts{
		PlanID: planID, ID: "0.0", Description: "do it",
		AcceptanceJSON: `{"command":"test -f sentinel.txt"}`,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	sessionID, workerID := mustCreateSessionAndWorkerForTest(t, s)
	if _, err := s.ClaimNextReady(ctx, planID, sessionID, workerID); err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}

	done, err := o.VerifyAndComplete(ctx, planID, "0.0", a2a.Evidence{Ran: "do it", Output: "ok"})
	if err != nil {
		t.Fatalf("VerifyAndComplete: %v", err)
	}
	if !done {
		t.Fatal("acceptance ran in the wrong directory: sentinel not found where the project dir is")
	}
}
