package orch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/a2a"
	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// Acceptance is the done-criteria a task's acceptance_json column
// describes. A task with an empty/absent Acceptance is judgment-only: its
// completion cannot be mechanically re-verified, so VerifyAndComplete
// treats a present, non-empty Evidence.Output as sufficient (there is no
// stronger signal available without a verifier agent — see the package
// doc's "prefer pure-Go verification where mechanical; a Ralph verifier
// only for judgment criteria" note. A judgment verifier is not implemented
// in this phase).
type Acceptance struct {
	// Command, if set, must exit 0 when re-run in the project directory.
	// This is the primary mechanical check: e.g. "go test ./..." or
	// "golangci-lint run".
	Command string `json:"command,omitempty"`

	// FileExists, if set, must exist (stat succeeds) in the project
	// directory for acceptance to pass.
	FileExists string `json:"file_exists,omitempty"`

	// Dir is the working directory Command runs in / FileExists is
	// resolved against. Empty means the orchestrator's configured project
	// directory (callers pass this in via VerifyOpts/AcceptanceChecker
	// wiring — see mechanicalAcceptanceCheck).
	Dir string `json:"dir,omitempty"`
}

// defaultAcceptanceJSON derives a first-cut Acceptance for a freshly
// materialized step task. Steps don't (yet) carry an explicit acceptance
// grammar in the plan markdown (that is a follow-on to Phase 6a's plan
// grammar), so absent a richer signal every step is judgment-only: no
// Command/FileExists is set, and VerifyAndComplete falls back to requiring
// non-empty evidence output. This keeps DispatchNext/VerifyAndComplete
// wiring complete end-to-end today while leaving room for a plan-level
// acceptance annotation later without changing the Acceptance shape.
func defaultAcceptanceJSON(_ plan.Step) (string, error) {
	return "", nil
}

// AcceptanceChecker re-runs a task's acceptance criteria in pure Go and
// reports whether it passes. dir is the project working directory the
// check should run in.
type AcceptanceChecker func(ctx context.Context, dir string, acceptanceJSON string, ev a2a.Evidence) (ok bool, reason string, err error)

// mechanicalAcceptanceCheck is the default AcceptanceChecker. For a
// MECHANICAL criterion (a shell command that must exit 0, or a file that
// must exist), it RE-RUNS the check itself — it never trusts ev.ExitCode
// or ev.Ran. For a task with no mechanical criterion (empty
// acceptanceJSON), it falls back to requiring non-empty evidence output,
// since there is nothing mechanical to re-verify and no judgment verifier
// is wired up in this phase.
func mechanicalAcceptanceCheck(ctx context.Context, dir string, acceptanceJSON string, ev a2a.Evidence) (bool, string, error) {
	if strings.TrimSpace(acceptanceJSON) == "" {
		if strings.TrimSpace(ev.Output) == "" {
			return false, "no acceptance criterion and no evidence output", nil
		}
		return true, "", nil
	}

	var acc Acceptance
	if err := json.Unmarshal([]byte(acceptanceJSON), &acc); err != nil {
		return false, "", fmt.Errorf("orch: unmarshal acceptance: %w", err)
	}

	checkDir := acc.Dir
	if checkDir == "" {
		checkDir = dir
	}

	if acc.FileExists != "" {
		if ok, reason, err := checkFileExists(checkDir, acc.FileExists); err != nil || !ok {
			return ok, reason, err
		}
	}

	if acc.Command != "" {
		return checkCommandExitsZero(ctx, checkDir, acc.Command)
	}

	return true, "", nil
}

func checkFileExists(dir, path string) (bool, string, error) {
	full := path
	if dir != "" && !filepath.IsAbs(path) {
		full = filepath.Join(dir, path)
	}
	if _, err := os.Stat(full); err != nil {
		return false, fmt.Sprintf("acceptance file %q does not exist: %v", full, err), nil
	}
	return true, "", nil
}

// checkCommandExitsZero RE-RUNS command in dir via a real shell exec and
// checks its exit status. This is the mechanical re-verification: the
// orchestrator never trusts a worker's self-reported exit code, it
// independently executes the acceptance command itself.
func checkCommandExitsZero(ctx context.Context, dir, command string) (bool, string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command) //nolint:gosec // G204: command is the task's own acceptance criterion (author-controlled plan content), not untrusted external input
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		reason := fmt.Sprintf("acceptance command %q failed: %v\n%s", command, err, strings.TrimSpace(string(out)))
		return false, reason, nil
	}
	return true, "", nil
}

// VerifyAndComplete is THE BACKBONE: it never trusts a worker's
// termination or self-report. It checks ev against task's acceptance
// criteria — re-running mechanical checks in pure Go — and only marks the
// task done in the store if verification passes. Otherwise it marks the
// task failed (retryable, per the task's normal retry budget) and emits a
// worker.verification_failed event carrying the rejection reason.
func (o *Orchestrator) VerifyAndComplete(ctx context.Context, planID, taskID string, ev a2a.Evidence) (done bool, err error) {
	task, err := o.store.GetTask(ctx, planID, taskID)
	if err != nil {
		return false, fmt.Errorf("orch: load task for verification: %w", err)
	}

	ok, reason, err := o.acceptanceCheck(ctx, o.projectDirFor(task), task.AcceptanceJSON, ev)
	if err != nil {
		return false, fmt.Errorf("orch: run acceptance check: %w", err)
	}

	sessionID := task.ClaimedBySession
	if sessionID == "" {
		sessionID, err = o.ensureOrchSession(ctx)
		if err != nil {
			return false, err
		}
	}

	if !ok {
		if _, err := o.store.MarkFailedWithPayload(ctx, planID, taskID, sessionID, store.EventPayload{
			Reason:    reason,
			Retryable: true,
		}, 3); err != nil {
			return false, fmt.Errorf("orch: mark failed on verification rejection: %w", err)
		}
		if err := o.store.Emit(ctx, store.EmitOpts{
			PlanID: planID,
			TaskID: taskID,
			Kind:   "worker.verification_failed",
			Stream: "orch",
			Actor:  "orchestrator",
			PayloadJSON: mustPayloadJSON(store.EventPayload{
				Reason: reason,
			}),
		}); err != nil {
			return false, fmt.Errorf("orch: emit verification_failed: %w", err)
		}
		return false, nil
	}

	evJSON, err := a2a.MarshalEvidence(ev)
	if err != nil {
		return false, fmt.Errorf("orch: marshal evidence for MarkDone: %w", err)
	}
	if _, err := o.store.MarkDone(ctx, planID, taskID, sessionID, evJSON); err != nil {
		return false, fmt.Errorf("orch: mark done: %w", err)
	}
	if err := o.store.Emit(ctx, store.EmitOpts{
		PlanID: planID,
		TaskID: taskID,
		Kind:   "worker.verified_done",
		Stream: "orch",
		Actor:  "orchestrator",
	}); err != nil {
		return false, fmt.Errorf("orch: emit verified_done: %w", err)
	}
	return true, nil
}

// projectDirFor resolves the working directory VerifyAndComplete's
// acceptance re-check should run in. In this phase there is no per-task
// working-directory column, so it always returns ".", i.e. the process's
// own working directory (the project checkout the orchestrator itself is
// running in). A future phase may thread a per-plan/per-project working
// directory from the store's project record.
func (o *Orchestrator) projectDirFor(_ *store.Task) string {
	return "."
}
