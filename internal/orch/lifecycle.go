package orch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agent"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// EnforcementPromptText is what an Orchestrator periodically writes to a
// running worker's stdin, per .agent-state/decisions.ndjson
// "context-management": rather than trying to detect when a CLI
// auto-compacts its context, send a periodic nudge that keeps the worker
// on task and, if it is itself capable of fanning out subagents/workflows,
// encourages it to do so; otherwise it should self-check at the next
// convenient point.
const EnforcementPromptText = "Stay on task. If you can fan out to subagents or workflows, do. Otherwise, self-check your progress against the assigned step now.\n"

// EnforcementPrompt runs a ticker for interval that writes
// EnforcementPromptText to a via WriteInput on every tick, until ctx is
// canceled or a is done. It never blocks waiting on the agent — WriteInput
// is a direct, non-blocking pty write, and a closed/exited agent's write
// error is swallowed (there is nothing left to nudge).
//
// Callers typically run this in its own goroutine alongside a dispatched
// worker's provider.Runner.Run call, canceling ctx when the worker
// finishes.
func EnforcementPrompt(ctx context.Context, a *agent.Agent, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.Done():
			return
		case <-ticker.C:
			// Best-effort: an error here means the agent's pty is already
			// gone, which a.Done() will observe on the next select
			// iteration. Never treat a failed nudge as fatal.
			_ = a.WriteInput([]byte(EnforcementPromptText))
		}
	}
}

// HandleContextEnd is called when a worker signals it has hit its own
// "manual end" (e.g. the underlying CLI's own end-of-context marker, or an
// operator-visible equivalent) rather than a stall or a normal turn
// completion. Per decision "context-management": kill the worker and
// re-dispatch fresh from plan-scoped context — this is cheap because all
// durable state lives in the store, not in the worker's own memory. The
// caller (DispatchNext's dispatch loop or an equivalent driver) is
// responsible for actually re-dispatching; HandleContextEnd's job is only
// to kill cleanly and release the task claim so it becomes ready again.
func (o *Orchestrator) HandleContextEnd(ctx context.Context, a *agent.Agent, planID, taskID, sessionID string) error {
	if err := a.Kill(); err != nil {
		// Killing an already-exited agent is not fatal — proceed to
		// release the claim regardless.
		_ = err
	}
	if err := o.store.MarkBlocked(ctx, planID, taskID, sessionID, store.EventPayload{
		Reason:         "worker hit context end; killed for fresh re-dispatch",
		Retryable:      true,
		OperatorAction: "requeue",
	}); err != nil && !errors.Is(err, store.ErrTaskNotOwnedRunning) {
		// A reclaimed+reassigned task's stale context-end is a benign no-op.
		return fmt.Errorf("orch: mark blocked on context end: %w", err)
	}
	return nil
}

// HandleWatchdogSignal reacts to one agent.Signal from agent.Watch per the
// control invariant: NEVER wait. A Prompt or Stall is treated as
// kill+reclaim (a worker that would block or has gone quiet cannot be
// trusted to make progress); ResourceExceeded is an immediate kill.
// Progress and Exited require no action here — Exited is handled by the
// dispatch loop's normal evidence/verification path, and Progress is a
// pure observation.
//
// Returns true if the caller should kill a and release the task claim
// (via HandleContextEnd or an equivalent MarkFailed/MarkBlocked call).
func HandleWatchdogSignal(sig agent.Signal) (shouldKill bool) {
	switch sig.Kind {
	case agent.Prompt, agent.Stall, agent.ResourceExceeded:
		return true
	case agent.Progress, agent.Exited:
		return false
	default:
		return false
	}
}

// decisionLogPath returns the path to worker workerID's XDG-ish decision
// log markdown file, rooted at o.decisionLogRoot (test-overridable) or the
// real XDG state root when unset.
func (o *Orchestrator) decisionLogPath(workerID string) (string, error) {
	root := o.decisionLogRoot
	if root == "" {
		var err error
		root, err = defaultDecisionLogRoot()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(root, "workers", workerID+".decisions.md"), nil
}

// defaultDecisionLogRoot resolves the real XDG state root for per-worker
// decision logs. Mirrors internal/xdg.StateRoot() rather than importing it
// directly, since xdg.StateRoot is repo/workspace-oriented and this needs
// only the bare state root.
func defaultDecisionLogRoot() (string, error) {
	if override := os.Getenv("RALPH_STATE_DIR"); override != "" {
		return filepath.Clean(override), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("orch: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "radioactive-ralph"), nil
}

// WriteWorkerDecision appends one decision line to workerID's XDG decision
// log markdown file, creating the file (and its parent directories) on
// first write. Each worker owns its own log; AbsorbDecisionLog later folds
// these into store events so the next dispatch's context can be informed
// by what a prior worker decided.
func (o *Orchestrator) WriteWorkerDecision(workerID, decision string) error {
	path, err := o.decisionLogPath(workerID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("orch: create decision log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // path is built from XDG state root + worker id, not user input
	if err != nil {
		return fmt.Errorf("orch: open decision log: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "- %s\n", decision); err != nil {
		return fmt.Errorf("orch: write decision log: %w", err)
	}
	return nil
}

// AbsorbDecisionLog reads workerID's XDG decision log (if any) and emits
// its content into the store's project event history as a single
// worker.decision_log event, so the orchestrator's NEXT dispatch — and any
// operator inspecting project history — can see what this worker decided
// without needing filesystem access to the XDG state root. Absorbing is
// idempotent-ish in effect (re-running it re-emits the same content as a
// new event row), so callers should call it once per worker lifecycle end
// (normal completion, verification rejection, or kill+reclaim).
//
// A missing decision log file is not an error — most workers write no
// decisions and that's fine; AbsorbDecisionLog is a no-op in that case.
func (o *Orchestrator) AbsorbDecisionLog(ctx context.Context, projectID, planID, taskID, workerID string) error {
	path, err := o.decisionLogPath(workerID)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is built from XDG state root + worker id, not user input
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("orch: read decision log: %w", err)
	}
	if len(raw) == 0 {
		return nil
	}
	return o.store.Emit(ctx, store.EmitOpts{
		ProjectID: projectID,
		PlanID:    planID,
		TaskID:    taskID,
		Kind:      "worker.decision_log",
		Stream:    "orch",
		Actor:     workerID,
		PayloadJSON: mustPayloadJSON(store.EventPayload{
			Summary: string(raw),
		}),
	})
}
