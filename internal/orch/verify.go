package orch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// acceptCommandRe matches an inline acceptance-command annotation in a plan
// step: a backtick-wrapped `accept: <shell command>` marker. The command is
// re-run by VerifyAndComplete and must exit 0 for the step to be accepted —
// this is what makes completion mechanically verified rather than "the
// worker produced some output".
var acceptCommandRe = regexp.MustCompile("`accept:\\s*([^`]+)`")

// acceptFileRe matches an inline `accept-file: <path>` marker: the named
// file (relative to the project dir) must exist for the step to be accepted.
var acceptFileRe = regexp.MustCompile("`accept-file:\\s*([^`]+)`")

// defaultAcceptanceJSON derives an Acceptance for a freshly materialized
// step task by scanning the step's text and detail for inline acceptance
// annotations — `accept: <command>` (a shell command re-run that must exit
// 0) and/or `accept-file: <path>` (a file that must exist). This keeps the
// heuristic-markdown philosophy: acceptance criteria live inline in the plan
// prose, not in a separate grammar file. A step with no annotation returns
// an empty acceptance (judgment-only: VerifyAndComplete falls back to
// requiring non-empty evidence output), so plans that don't opt into
// mechanical checks still work — but any step that DOES carry an annotation
// is genuinely re-verified, closing the "any non-empty output passes" gap.
func defaultAcceptanceJSON(step plan.Step) (string, error) {
	haystack := step.Text
	if step.Detail != "" {
		haystack += "\n" + step.Detail
	}

	var acc Acceptance
	if m := acceptCommandRe.FindStringSubmatch(haystack); m != nil {
		acc.Command = strings.TrimSpace(m[1])
	}
	if m := acceptFileRe.FindStringSubmatch(haystack); m != nil {
		acc.FileExists = strings.TrimSpace(m[1])
	}

	if acc.Command == "" && acc.FileExists == "" {
		return "", nil
	}
	raw, err := json.Marshal(acc)
	if err != nil {
		return "", fmt.Errorf("orch: marshal derived acceptance: %w", err)
	}
	return string(raw), nil
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
//
// NOT CONTAINED, deliberately, and the asymmetry with provider turns is the
// point. Write containment confines the PROVIDER — untrusted output acting on
// the checkout. This is the orchestrator running the plan author's own
// acceptance criterion, and acceptance commands routinely and legitimately
// write outside the project: `go build` populates GOCACHE under the user's home,
// test runners write to TMPDIR. Wrapping this in the project-root policy would
// fail those commands and report the task unverified for a reason that has
// nothing to do with the task.
//
// The exposure is real but bounded and different in kind: plan markdown is
// author-controlled config, the same trust level as the binding that chooses
// which CLI to launch. If that ever stops holding — plans imported from an
// untrusted source — this needs its own policy with a wider root (project plus
// caches), not the provider one. Recorded during the 2026-07-28 exec-path audit
// rather than changed silently.
func checkCommandExitsZero(ctx context.Context, dir, command string) (bool, string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command) //nolint:gosec // G204: command is the task's own acceptance criterion (author-controlled plan content), not untrusted external input
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		reason := fmt.Sprintf("acceptance command %q failed: %v\n%s", command, err, strings.TrimSpace(string(out)))
		if phantom := phantomFindingPaths(dir, string(out)); len(phantom) > 0 {
			reason += fmt.Sprintf(
				"\n\nNOTE: %d of these findings name files that DO NOT EXIST: %s\n"+
					"A tool reporting a finding for a path that is not on disk is "+
					"reporting a stale index, not a defect. Clear the tool's cache "+
					"(e.g. `golangci-lint cache clean`) and re-run before changing "+
					"code -- the finding cannot be fixed, because the file is gone.",
				len(phantom), strings.Join(phantom, ", "))
		}
		return false, reason, nil
	}
	return true, "", nil
}

// findingPathRe matches the near-universal "path:line:col:" diagnostic prefix
// emitted by go vet, golangci-lint, gcc, tsc, and friends. Anchored per line so
// a path mentioned mid-sentence in prose is not mistaken for a finding.
//
// The optional `[A-Za-z]:` prefix is a Windows drive letter, and leaving it out
// was a real bug: the path body cannot contain `:`, so `C:\src\x.go:9:1:` did
// not match AT ALL and every Windows finding was invisible. It passed on macOS
// and Linux and failed only on the Windows runner -- the platform where a
// colon is part of an ordinary absolute path.
var findingPathRe = regexp.MustCompile(`(?m)^\s*((?:[A-Za-z]:)?[^\s:][^:]*\.[A-Za-z0-9_]+):\d+:(?:\d+:)?\s`)

// phantomFindingPaths returns the distinct file paths a tool reported findings
// for that do not exist on disk.
//
// This exists because a stale linter cache invents work. golangci-lint once
// reported 11 findings, every one under a sibling directory that had been
// deleted; `cache clean` reduced the same command to "0 issues". The step
// failed, a provider turn was handed those findings, and it tried to fix files
// that were not there -- burning the turn and producing a plausible, compiling
// change for a defect that did not exist.
//
// The agent behaved correctly. The defect is that an IMPOSSIBLE task looks
// exactly like a hard one, so nothing surfaced the contradiction until a human
// noticed the paths pointed outside the project.
//
// Deliberately reports rather than suppresses: the command still FAILED and the
// step still fails. Turning a red step green on a heuristic would be far worse
// than the ambiguity being fixed -- a mis-parse would hide real findings. This
// only adds an explanation to a failure that was going to happen anyway.
func phantomFindingPaths(dir, output string) []string {
	var phantom []string
	seen := map[string]bool{}
	for _, m := range findingPathRe.FindAllStringSubmatch(output, -1) {
		path := strings.TrimSpace(m[1])
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		// Relative paths are resolved against dir, which is exactly the working
		// directory the command ran in (cmd.Dir above). Skipping them was the
		// first version's bug: golangci-lint reports the real stale-cache paths
		// RELATIVELY ("../.worktrees/..."), so the detector missed the very case
		// it was written for and only caught the synthetic absolute-path
		// fixture. Checked against the real captured output, not just the test.
		resolved := path
		if !filepath.IsAbs(resolved) {
			if dir == "" {
				continue
			}
			resolved = filepath.Join(dir, resolved)
		}
		if _, err := os.Stat(resolved); err != nil && os.IsNotExist(err) {
			phantom = append(phantom, path)
		}
	}
	return phantom
}

// VerifyAndComplete is THE BACKBONE: it never trusts a worker's
// termination or self-report. It checks ev against task's acceptance
// criteria — re-running mechanical checks in pure Go — and only marks the
// task done in the store if verification passes. Otherwise it marks the
// task failed (retryable, per the task's normal retry budget) and emits a
// worker.verification_failed event carrying the rejection reason.
func (o *Orchestrator) VerifyAndComplete(ctx context.Context, planID, taskID string, ev a2a.Evidence) (done bool, err error) {
	// No reporting session named: fall back to whoever owns the task now. This
	// is the ORCHESTRATOR-initiated path (verification it runs itself), where
	// there is no separate reporter to attribute the result to.
	return o.VerifyAndCompleteAs(ctx, planID, taskID, "", ev)
}

// VerifyAndCompleteAs verifies evidence submitted BY reportingSession and
// completes the task only if that session still owns it.
//
// The session has to be passed in rather than read from the task. store.MarkDone
// and MarkFailed are owner-guarded — they require claimed_by_session to match
// the session they are GIVEN — and reading the current owner here satisfied that
// guard with a session that did not produce the evidence. So when a worker
// stalled, was reaped, and its replacement claimed the task, the stale worker's
// late result was written under the REPLACEMENT's session: the guard passed, the
// replacement's attempt was overwritten, and ErrTaskNotOwnedRunning never fired,
// so nothing reported the loss (#248).
//
// An empty reportingSession keeps the old behavior for orchestrator-initiated
// verification, which has no separate reporter.
func (o *Orchestrator) VerifyAndCompleteAs(
	ctx context.Context,
	planID, taskID, reportingSession string,
	ev a2a.Evidence,
) (done bool, err error) {
	task, err := o.store.GetTask(ctx, planID, taskID)
	if err != nil {
		return false, fmt.Errorf("orch: load task for verification: %w", err)
	}

	dir, err := o.projectDirFor(ctx, task.PlanID)
	if err != nil {
		return false, err
	}
	// Acceptance verification gets its OWN budget, detached from whatever
	// deadline the caller arrived with.
	//
	// The caller is usually the post-run path, whose persistCtx is bounded for
	// STORE WRITES. But verification re-runs the step's acceptance command, and a
	// real plan's command can be a test suite. A live self-test had a step whose
	// command took 30s warm and 138s under load against a 30s persist budget: it
	// could not fit, the context expired here, the task was never marked, and it
	// sat 'running' with a dead heartbeat until the reaper requeued work that had
	// already SUCCEEDED. Six times in one run, reading as a flaky step.
	//
	// Detached rather than merely lengthened, because the shutdown case that
	// motivated persistCtx applies just as much here: a turn that has produced a
	// result must get its verdict recorded even as the supervisor tears down.
	verifyCtx, cancelVerify := context.WithTimeout(
		context.WithoutCancel(ctx), o.effectiveVerificationBudget())
	defer cancelVerify()

	// KEEP BEATING while it runs. The heartbeat goroutine stops the instant the
	// provider turn returns, and verification happens after that -- so without
	// this, a long acceptance command runs with nothing beating and the reaper
	// reclaims the task at 90s. The owner-guarded MarkDone below then becomes a
	// benign no-op and successful work is silently requeued.
	//
	// A longer verification budget WITHOUT this makes that strictly worse: a
	// 10-minute window against a 90-second stale threshold guarantees the
	// reclaim it was meant to prevent. The budget and the heartbeat move
	// together or not at all.
	var ok bool
	var reason string
	beatErr := o.beatWhile(verifyCtx, task.ClaimedByWorkerID, func() error {
		var checkErr error
		ok, reason, checkErr = o.acceptanceCheck(verifyCtx, dir, task.AcceptanceJSON, ev)
		return checkErr
	})
	if beatErr != nil {
		// Name a budget exhaustion as such. A bare "context deadline exceeded"
		// here reads exactly like the acceptance command failing on its own
		// terms, and that ambiguity is why this bug took four explanations to
		// find: an operator could not tell "your suite is failing" from "your
		// suite did not get to finish".
		if errors.Is(beatErr, context.DeadlineExceeded) {
			return false, fmt.Errorf(
				"orch: acceptance verification exceeded its %s budget for task %s/%s; "+
					"the command may be slow rather than failing -- time it directly, "+
					"and note this budget is separate from the provider stall lease: %w",
				o.effectiveVerificationBudget(), planID, taskID, beatErr)
		}
		return false, fmt.Errorf("orch: run acceptance check: %w", beatErr)
	}

	// From here on, everything RECORDS THE VERDICT that verification just
	// produced, so it gets a fresh detached budget of its own.
	//
	// Detaching verification alone was not enough, and the test caught it: a
	// slow-but-passing acceptance command legitimately consumes the caller's
	// entire budget, so MarkDone then failed with "context deadline exceeded"
	// and the task stayed unmarked -- the same lost-verdict bug one step later.
	// A verdict that cannot be written is indistinguishable from no verdict, and
	// that is precisely what the reaper requeues.
	ctx, cancelPersist := context.WithTimeout(
		context.WithoutCancel(ctx), o.effectivePersistBudget())
	defer cancelPersist()

	// Completion-time containment. This is the guarantee Ralph actually makes:
	// pre-dispatch validation cannot stop a provider — a separate process
	// writing by pathname minutes later — from having its declared output
	// redirected by a peer replacing a directory component. What Ralph CAN do
	// is refuse to call such a task done.
	//
	// Only checked when acceptance PASSED: a task that already failed is not
	// about to be marked done, so re-checking would only cost filesystem work.
	// Ordering acceptance first also means the more specific rejection wins.
	if ok {
		if escaped := o.verifyTaskOutputContainment(ctx, task, dir); escaped != "" {
			ok = false
			reason = escaped
		}
	}

	// A named reporter is used VERBATIM, so the store's owner guard compares
	// against the session that actually produced this evidence. A stale reporter
	// then loses benignly, which is the designed behavior.
	sessionID := reportingSession
	if sessionID == "" {
		sessionID = task.ClaimedBySession
	}
	if sessionID == "" {
		sessionID, err = o.ensureOrchSession(ctx)
		if err != nil {
			return false, err
		}
	}

	if !ok {
		// If the task was reclaimed/reassigned out from under this session
		// while acceptance ran, MarkFailed is a benign no-op — the current
		// owner's attempt stands (see store.ErrTaskNotOwnedRunning).
		if _, err := o.store.MarkFailedWithPayload(ctx, planID, taskID, sessionID, store.EventPayload{
			Reason:    reason,
			Retryable: true,
		}, 3); err != nil {
			if !errors.Is(err, store.ErrTaskNotOwnedRunning) {
				return false, fmt.Errorf("orch: mark failed on verification rejection: %w", err)
			}
			// A stale reporter lost the task to a replacement. The mark is a
			// benign no-op — and the event must be suppressed with it, or a
			// reaped worker's late rejection announces a failure against a task
			// whose CURRENT owner is still running. The store stays correct while
			// the event stream lies, which is worse than either alone: an
			// operator reads the event, not the row.
			return false, nil
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
		// If the task was reclaimed and reassigned to another session while
		// this (possibly slow) acceptance check ran, MarkDone is a benign
		// no-op — the current owner's attempt stands; we must NOT report this
		// stale completion as done. Mirrors the rejection path's handling of
		// the same race.
		if errors.Is(err, store.ErrTaskNotOwnedRunning) {
			return false, nil
		}
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

// projectDirFor resolves the working directory an acceptance re-check (and a
// dispatched worker) should run in for the plan's owning project: the
// project's recorded abs_path checkout, NOT the orchestrator process's own
// cwd. Supervisor mode's working directory is deliberately irrelevant (§4),
// so trusting "." would run acceptance commands and workers against wherever
// the supervisor service happened to be started — commonly not any project
// at all. When the project has no recorded abs_path (should not happen for a
// project created via --init, which always seeds one) it falls back to "."
// so a bare/test project without a fingerprint still runs somewhere rather
// than erroring.
func (o *Orchestrator) projectDirFor(ctx context.Context, planID string) (string, error) {
	p, err := o.store.GetPlan(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("orch: load plan for project dir: %w", err)
	}
	dir, found, err := o.store.ProjectAbsPath(ctx, p.ProjectID)
	if err != nil {
		return "", err
	}
	if !found || dir == "" {
		return ".", nil
	}
	return dir, nil
}

// verifyTaskOutputContainment re-checks every declared output against the
// project root at completion, returning a rejection reason or "".
//
// A declared output that no longer resolves inside the project means the write
// landed somewhere the plan never authorized — whether through a symlinked
// ancestor planted after admission or a path that was never contained. Marking
// such a task done would launder an escape into a success.
//
// A RESOLUTION FAULT is not treated as an escape here. Refusing completion for
// a transient I/O error would fail honest work; the pre-dispatch gate makes the
// same distinction for the same reason.
func (o *Orchestrator) verifyTaskOutputContainment(ctx context.Context, task *store.Task, projectDir string) string {
	step, ok := o.stepForTask(ctx, task)
	if !ok {
		return ""
	}
	decl := declFromMetadata(step)
	for _, out := range decl.Outputs {
		if _, err := secureProjectPath(projectDir, out); err != nil {
			if errors.Is(err, ErrTaskPathEscapesProject) {
				return fmt.Sprintf(
					"declared output %q does not resolve inside the project", out)
			}
			// Unresolvable for a non-containment reason: do not fail honest
			// work on a transient fault.
			return ""
		}
	}
	return ""
}

// stepForTask resolves the plan step a task was materialized from, so
// completion can read its declared outputs. Returns false when the plan source
// cannot be resolved — in which case there is nothing to check rather than
// something to reject.
func (o *Orchestrator) stepForTask(ctx context.Context, task *store.Task) (plan.Step, bool) {
	storedPlan, err := o.store.GetPlan(ctx, task.PlanID)
	if err != nil || storedPlan.SourceMarkdown == "" {
		return plan.Step{}, false
	}
	parsed, err := plan.Parse([]byte(storedPlan.SourceMarkdown))
	if err != nil {
		return plan.Step{}, false
	}
	located, found := indexPlanSteps(parsed)[task.ID]
	if !found {
		return plan.Step{}, false
	}
	return located.step, true
}
