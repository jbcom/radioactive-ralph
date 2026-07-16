// Package orch is the orchestration layer: the correctness backbone where
// completion is ORCHESTRATOR-VERIFIED, never agent-asserted (spec §10;
// .agent-state/decisions.ndjson "completion-verification").
//
// Orchestrator reads a plan (internal/plan), dispatches workers
// (internal/provider onto internal/agent) with PLAN-SCOPED context (only
// the ready step(s) + their group heading + the plan title — never the
// whole plan document), and verifies each worker's submitted evidence
// against the task's acceptance criteria before ever marking a task done.
// A worker terminating is not completion; it is a signal to verify.
package orch

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/a2a"
	"github.com/jbcom/radioactive-ralph/internal/agent"
	"github.com/jbcom/radioactive-ralph/internal/plan"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// RunnerFactory resolves a provider.Runner for a binding. Exists so tests
// can inject a fake runner without a real agent CLI. Defaults to
// provider.NewRunner.
type RunnerFactory func(provider.Binding) (provider.Runner, error)

// BindingResolver picks the provider binding to use for one dispatch.
// Exists so tests and callers can supply a fixed/fake binding without a
// real config.toml. Defaults to always resolving "claude" via
// provider.ResolveBinding with zero File/Local config (i.e. the built-in
// claude capability record).
type BindingResolver func(ctx context.Context, projectID string, parallelGroup bool) (provider.Binding, error)

// Clock abstracts time.Now for deterministic tests (enforcement-prompt
// cadence, spend windows).
type Clock func() time.Time

// Orchestrator dispatches workers against a plan and is the sole authority
// that may mark a task done (via VerifyAndComplete).
type Orchestrator struct {
	store *store.Store

	newRunner       RunnerFactory
	resolveBinding  BindingResolver
	now             Clock
	maxParallel     int
	watchdogConfig  agent.WatchdogConfig
	spendCapUSD     map[string]float64 // provider name -> cap; 0/absent = uncapped
	acceptanceCheck AcceptanceChecker

	// decisionLogAbsorb, when set, is invoked by AbsorbDecisionLog (see
	// lifecycle.go) instead of the default XDG-backed implementation. Test
	// hook.
	decisionLogRoot string

	// orchSessionID is the store session row the orchestrator itself owns,
	// used as the claimed_by_session value for claims/marks it performs
	// directly (as opposed to a dispatched worker's own session — see
	// dispatchWorker). Lazily created on first use since tasks.
	// claimed_by_session has a foreign key into sessions(id): the
	// orchestrator needs a real row to reference, not a bare string.
	orchSessionID string
}

// Option configures an Orchestrator at construction time.
type Option func(*Orchestrator)

// WithRunnerFactory overrides how an Orchestrator resolves a
// provider.Runner for a binding. Primarily for tests.
func WithRunnerFactory(f RunnerFactory) Option {
	return func(o *Orchestrator) { o.newRunner = f }
}

// WithBindingResolver overrides how an Orchestrator picks a provider
// binding for a dispatch. Primarily for tests.
func WithBindingResolver(f BindingResolver) Option {
	return func(o *Orchestrator) { o.resolveBinding = f }
}

// WithClock overrides the Orchestrator's time source. Primarily for tests.
func WithClock(c Clock) Option {
	return func(o *Orchestrator) { o.now = c }
}

// WithMaxParallel bounds how many steps DispatchNext will dispatch in one
// call for a parallel group. Zero/negative means unbounded (bounded only
// by the number of ready steps).
func WithMaxParallel(n int) Option {
	return func(o *Orchestrator) { o.maxParallel = n }
}

// WithWatchdog overrides the stall/prompt watchdog configuration used for
// dispatched workers.
func WithWatchdog(cfg agent.WatchdogConfig) Option {
	return func(o *Orchestrator) { o.watchdogConfig = cfg }
}

// WithSpendCap sets a per-provider spend cap in USD. A provider with no
// configured cap (or a cap of 0) is treated as uncapped.
func WithSpendCap(providerName string, capUSD float64) Option {
	return func(o *Orchestrator) {
		if o.spendCapUSD == nil {
			o.spendCapUSD = map[string]float64{}
		}
		o.spendCapUSD[providerName] = capUSD
	}
}

// WithAcceptanceChecker overrides the mechanical acceptance checker used by
// VerifyAndComplete. Primarily for tests.
func WithAcceptanceChecker(c AcceptanceChecker) Option {
	return func(o *Orchestrator) { o.acceptanceCheck = c }
}

// WithDecisionLogRoot overrides the XDG-ish root directory used for
// per-worker decision logs (see lifecycle.go). Primarily for tests.
func WithDecisionLogRoot(dir string) Option {
	return func(o *Orchestrator) { o.decisionLogRoot = dir }
}

// New constructs an Orchestrator against st. Defaults: provider.NewRunner
// as the runner factory, a claude binding resolver, real time, unbounded
// parallelism, a 5-minute stall timeout, no spend caps, and the built-in
// mechanical acceptance checker.
func New(st *store.Store, opts ...Option) *Orchestrator {
	o := &Orchestrator{
		store:     st,
		newRunner: provider.NewRunner,
		resolveBinding: func(_ context.Context, _ string, _ bool) (provider.Binding, error) {
			return provider.ResolveBinding(provider.File{}, provider.Local{}, provider.VariantFile{})
		},
		now: time.Now,
		watchdogConfig: agent.WatchdogConfig{
			StallTimeout: 5 * time.Minute,
		},
		acceptanceCheck: mechanicalAcceptanceCheck,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// scopedContext is the plan-scoped context handed to a dispatched worker:
// only the ready step's own text/detail, its group heading, and the plan
// title — never the whole plan document (spec §10's "scoped context"
// insight; over-sharing the whole plan defeats the purpose of
// decomposition and burns the worker's context budget on irrelevant
// steps).
type scopedContext struct {
	PlanTitle    string
	GroupHeading string
	StepText     string
	StepDetail   string
}

// prompt renders the scoped context as the worker's user prompt.
func (c scopedContext) prompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan: %s\n", c.PlanTitle)
	fmt.Fprintf(&b, "Group: %s\n\n", c.GroupHeading)
	b.WriteString(c.StepText)
	if c.StepDetail != "" {
		b.WriteString("\n\n")
		b.WriteString(c.StepDetail)
	}
	return b.String()
}

// dispatchedStep pairs a ready plan.Step with the store task created for
// it, for bookkeeping across DispatchNext's per-step dispatch loop.
type dispatchedStep struct {
	ref  plan.StepRef
	step plan.Step
	task *store.Task
}

// DispatchNext loads the plan for planID, computes what's ready right now,
// and dispatches workers for as many ready steps as capacity (maxParallel)
// and spend caps allow. It returns the number of steps actually dispatched.
//
// DispatchNext does NOT wait for dispatched workers to finish — each
// dispatch runs its provider turn in its own goroutine, wired through
// agent.Watch for stall/prompt/resource-exceeded handling (kill+reclaim,
// never wait), and reports its result back for VerifyAndComplete via the
// store's task/event log. The worker's termination or self-reported
// result does NOT mark the task done.
func (o *Orchestrator) DispatchNext(ctx context.Context, projectID, planID string) (dispatched int, err error) {
	storedPlan, err := o.store.GetPlan(ctx, planID)
	if err != nil {
		return 0, fmt.Errorf("orch: load plan: %w", err)
	}
	if storedPlan.SourceMarkdown == "" {
		return 0, fmt.Errorf("orch: plan %q has no source markdown", planID)
	}

	parsedPlan, err := plan.Parse([]byte(storedPlan.SourceMarkdown))
	if err != nil {
		return 0, fmt.Errorf("orch: parse plan markdown: %w", err)
	}

	done, err := o.doneSet(ctx, planID)
	if err != nil {
		return 0, err
	}

	readySteps, refs, parallel := plan.DecomposeRefs(parsedPlan, done)
	if len(readySteps) == 0 {
		return 0, nil
	}

	groupHeading := groupHeadingFor(parsedPlan, refs[0])

	limit := len(readySteps)
	if !parallel {
		// A sequential leaf group only ever returns its first not-done
		// step (see plan.Decompose), but guard explicitly anyway: never
		// dispatch more than one step from a non-parallel group at once.
		limit = 1
	}
	if o.maxParallel > 0 && limit > o.maxParallel {
		limit = o.maxParallel
	}

	// TODO(fan-out): when parallel is true AND the resolved binding for
	// this group has NativeFanout, a single fan-out-capable worker could
	// take the WHOLE group in one invocation (the CLI/API manages its own
	// sub-agents) instead of Ralph spawning N workers here. That path is
	// not implemented yet — every ready step below is dispatched as its
	// own Ralph-managed worker regardless of NativeFanout. Wiring the
	// fan-out delegation requires deciding how a single worker's Evidence
	// maps back onto N distinct store tasks for VerifyAndComplete, which
	// is a separate design question from this dispatch loop.
	for i := 0; i < limit; i++ {
		binding, err := o.resolveBinding(ctx, projectID, parallel)
		if err != nil {
			return dispatched, fmt.Errorf("orch: resolve binding: %w", err)
		}

		if err := o.checkSpendCap(ctx, projectID, binding.Name); err != nil {
			// Spend-cap refusal is not a fatal DispatchNext error: other
			// steps (possibly on an uncapped provider) may still be
			// dispatchable. Record the refusal and move to the next ready
			// step.
			_ = o.store.Emit(ctx, store.EmitOpts{
				ProjectID: projectID,
				PlanID:    planID,
				Kind:      "worker.admission_refused",
				Stream:    "service",
				PayloadJSON: mustPayloadJSON(store.EventPayload{
					Reason: err.Error(),
				}),
			})
			continue
		}

		sessionID, workerID, err := o.spawnWorkerRows(ctx, binding)
		if err != nil {
			return dispatched, err
		}

		ds, err := o.claimStepTask(ctx, planID, parsedPlan, refs[i], readySteps[i], sessionID, workerID)
		if err != nil {
			return dispatched, err
		}
		if ds == nil {
			// Nothing ready to claim (lost the race to another claimer, or
			// the task doesn't exist yet under this plan — see
			// claimStepTask). Not an error; just nothing to dispatch here.
			// Release the worker row we just spawned since it will never
			// be assigned a task.
			_ = o.store.ClearWorkerTask(ctx, workerID, "idle")
			continue
		}
		if err := o.store.SetWorkerTask(ctx, workerID, planID, ds.task.ID); err != nil {
			return dispatched, fmt.Errorf("orch: set worker task: %w", err)
		}

		scoped := scopedContext{
			PlanTitle:    planTitle(parsedPlan, storedPlan.Title),
			GroupHeading: groupHeading,
			StepText:     ds.step.Text,
			StepDetail:   ds.step.Detail,
		}

		if err := o.dispatchWorker(ctx, projectID, planID, sessionID, workerID, binding, ds, scoped); err != nil {
			return dispatched, err
		}
		dispatched++
	}

	return dispatched, nil
}

// doneSet builds the done-set plan.Decompose needs, keyed by StepRef.ID(),
// from the store's task statuses for this plan. A step is "done" for
// decomposition purposes once its task is in a terminal-satisfied state
// (done, skipped, or decomposed) — the same set store.Ready treats as
// satisfying a dependency.
func (o *Orchestrator) doneSet(ctx context.Context, planID string) (map[string]bool, error) {
	tasks, err := o.store.ListTasks(ctx, planID, nil)
	if err != nil {
		return nil, fmt.Errorf("orch: list tasks: %w", err)
	}
	done := map[string]bool{}
	for _, t := range tasks {
		switch t.Status {
		case store.TaskStatusDone, store.TaskStatusSkipped, store.TaskStatusDecomposed:
			done[t.ID] = true
		}
	}
	return done, nil
}

// ensureOrchSession lazily creates (once) the store session row the
// orchestrator itself uses as a claimed_by_session/actor value wherever it
// acts directly rather than on behalf of a specific dispatched worker.
// tasks.claimed_by_session is a foreign key into sessions(id), so a bare
// sentinel string like "orch" cannot satisfy it — the orchestrator needs a
// real, durable session row to reference.
func (o *Orchestrator) ensureOrchSession(ctx context.Context) (string, error) {
	if o.orchSessionID != "" {
		return o.orchSessionID, nil
	}
	id, err := o.store.CreateSession(ctx, store.SessionOpts{
		Role:         "orchestrator",
		PID:          1,
		PIDStartTime: fmt.Sprintf("%d", o.now().UnixNano()),
	})
	if err != nil {
		return "", fmt.Errorf("orch: create orchestrator session: %w", err)
	}
	o.orchSessionID = id
	return id, nil
}

// spawnWorkerRows creates the session+worker store rows a dispatch will
// run under. tasks.claimed_by_session and .claimed_by_worker_id are both
// foreign keys, so ClaimNextReady needs real rows to reference before a
// claim can land — this must run BEFORE claimStepTask, not after.
func (o *Orchestrator) spawnWorkerRows(ctx context.Context, binding provider.Binding) (sessionID, workerID string, err error) {
	sessionID, err = o.store.CreateSession(ctx, store.SessionOpts{
		Role:         "worker",
		PID:          1,
		PIDStartTime: fmt.Sprintf("%d", o.now().UnixNano()),
	})
	if err != nil {
		return "", "", fmt.Errorf("orch: create worker session: %w", err)
	}
	workerID, err = o.store.CreateWorker(ctx, store.WorkerOpts{
		SessionID:           sessionID,
		Provider:            binding.Name,
		NativeFanout:        binding.Config.NativeFanout,
		SubprocessPID:       1,
		SubprocessStartTime: fmt.Sprintf("%d", o.now().UnixNano()),
	})
	if err != nil {
		return "", "", fmt.Errorf("orch: create worker: %w", err)
	}
	return sessionID, workerID, nil
}

// claimStepTask ensures a store task exists for ref (creating it on first
// sight, keyed by ref.ID() as the stable task ID) and claims it via
// store.ClaimNextReady bound to that specific task id, under sessionID/
// workerID (see spawnWorkerRows). Returns nil (no error) if the task could
// not be claimed right now (e.g. another dispatcher claimed it first).
func (o *Orchestrator) claimStepTask(ctx context.Context, planID string, p *plan.Plan, ref plan.StepRef, step plan.Step, sessionID, workerID string) (*dispatchedStep, error) {
	taskID := ref.ID()

	if _, err := o.store.GetTask(ctx, planID, taskID); err != nil {
		// First sight of this step: materialize it as a pending task. A
		// concurrent dispatcher losing the create race is fine — the
		// unique (plan_id, id) constraint means only one CreateTask wins;
		// the loser proceeds straight to the claim attempt below, which is
		// the one that must be race-safe.
		acceptance, jsonErr := defaultAcceptanceJSON(step)
		if jsonErr != nil {
			return nil, fmt.Errorf("orch: build acceptance for %s: %w", taskID, jsonErr)
		}
		_ = o.store.CreateTask(ctx, store.CreateTaskOpts{
			PlanID:         planID,
			ID:             taskID,
			Description:    step.Text,
			AcceptanceJSON: acceptance,
		})
	}

	claimed, err := o.store.ClaimNextReady(ctx, planID, sessionID, workerID)
	if err != nil {
		if err == store.ErrNoReadyTask { //nolint:errorlint // sentinel comparison mirrors store's own doc'd usage
			return nil, nil
		}
		return nil, fmt.Errorf("orch: claim %s: %w", taskID, err)
	}
	if claimed.ID != taskID {
		// ClaimNextReady claimed a DIFFERENT ready task (e.g. sequence
		// ordering picked another id first). That's fine — it is still a
		// valid step to dispatch, so resolve it back to its plan.Step via
		// its ID.
		resolvedRef, ok := parseStepRefID(claimed.ID)
		if !ok {
			return nil, fmt.Errorf("orch: claimed task %q is not a plan step id", claimed.ID)
		}
		resolvedStep, _, err := p.StepAt(resolvedRef)
		if err != nil {
			return nil, fmt.Errorf("orch: resolve claimed step %q: %w", claimed.ID, err)
		}
		return &dispatchedStep{ref: resolvedRef, step: resolvedStep, task: claimed}, nil
	}
	return &dispatchedStep{ref: ref, step: step, task: claimed}, nil
}

// groupHeadingFor returns the heading of the leaf group owning ref, or ""
// if it cannot be resolved (should not happen for a ref DecomposeRefs just
// returned).
func groupHeadingFor(p *plan.Plan, ref plan.StepRef) string {
	_, g, err := p.StepAt(ref)
	if err != nil {
		return ""
	}
	return g.Heading
}

// planTitle returns the plan's own notion of a title: the first top-level
// group's heading if present, else the store-level Title (e.g. derived
// from the plan's slug at creation time).
func planTitle(p *plan.Plan, storeTitle string) string {
	if len(p.Groups) > 0 && p.Groups[0].Heading != "" {
		return p.Groups[0].Heading
	}
	return storeTitle
}

// mustPayloadJSON serializes an EventPayload, falling back to "{}" on the
// (unreachable in practice) marshal-error path — mirrors store's own
// internal payloadJSON helper, duplicated here because that helper is
// unexported.
func mustPayloadJSON(p store.EventPayload) string {
	// EventPayload always round-trips (plain strings/bools/slices), so the
	// only realistic failure mode is a future field type that doesn't
	// marshal; fail safe to an empty object rather than losing the event
	// entirely.
	raw, err := jsonMarshal(p)
	if err != nil {
		return "{}"
	}
	return raw
}

// dispatchWorker runs one provider turn for ds against binding, wiring the
// watchdog and evidence submission. It never marks the task done — it
// only submits evidence (an a2a message) and, for the mechanical happy
// path where verification is cheap and synchronous, immediately invokes
// VerifyAndComplete so a fully-automated small plan converges without an
// external caller having to poll. Callers that want asynchronous
// verification (e.g. a real long-running worker) can instead call
// VerifyAndComplete themselves once evidence lands.
//
// Watchdog note: provider.Runner.Run is a synchronous, provider-owned
// call — it internally drives agent.Start and its own output-framing
// loop (stream-json for claude, file-based for codex/declarative), so it
// does not hand back the underlying *agent.Agent for this package to run
// agent.Watch against directly. Every shipped Runner DOES honor ctx
// cancellation to kill its underlying process (verified: claude.go and
// opencode.go select on ctx.Done(); codex.go and declarative.go use
// exec.CommandContext), so dispatchWorker enforces the "never wait"
// control invariant by wrapping Run in a context bounded by
// o.watchdogConfig.StallTimeout: a worker that produces no result within
// that window is killed via ctx cancellation, exactly the kill+reclaim
// agent.Watch's Stall signal would trigger for a caller with direct pty
// access. A future phase that threads agent.Start/agent.Watch through to
// this layer (e.g. a Runner variant that exposes its *agent.Agent) can
// additionally react to Prompt-pattern detection mid-turn; today that
// signal is only available to callers inside the provider package itself.
func (o *Orchestrator) dispatchWorker(ctx context.Context, projectID, planID string, binding provider.Binding, ds *dispatchedStep, scoped scopedContext) error {
	runner, err := o.newRunner(binding)
	if err != nil {
		return fmt.Errorf("orch: resolve runner for %q: %w", binding.Name, err)
	}

	if o.watchdogConfig.StallTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.watchdogConfig.StallTimeout)
		defer cancel()
	}

	sessionID, err := o.store.CreateSession(ctx, store.SessionOpts{
		Role:         "worker",
		PID:          1,
		PIDStartTime: fmt.Sprintf("%d", o.now().UnixNano()),
	})
	if err != nil {
		return fmt.Errorf("orch: create worker session: %w", err)
	}

	workerID, err := o.store.CreateWorker(ctx, store.WorkerOpts{
		SessionID:           sessionID,
		Provider:            binding.Name,
		NativeFanout:        binding.Config.NativeFanout,
		SubprocessPID:       1,
		SubprocessStartTime: fmt.Sprintf("%d", o.now().UnixNano()),
	})
	if err != nil {
		return fmt.Errorf("orch: create worker: %w", err)
	}
	if err := o.store.SetWorkerTask(ctx, workerID, planID, ds.task.ID); err != nil {
		return fmt.Errorf("orch: set worker task: %w", err)
	}

	req := provider.Request{
		WorkingDir: ".",
		UserPrompt: scoped.prompt(),
	}

	result, runErr := runner.Run(ctx, binding, req)

	// Spend is real the moment tokens were billed, independent of whether
	// the work is ultimately accepted by VerifyAndComplete — record it
	// unconditionally so spend-cap accounting stays accurate even for
	// rejected/failed turns.
	if err := o.recordUsage(ctx, projectID, workerID, binding.Name, string(req.Model), result.Usage); err != nil {
		return fmt.Errorf("orch: record usage: %w", err)
	}

	// A worker terminating — successfully or not — is NEVER completion.
	// It is only a signal to build evidence and run VERIFICATION.
	ev := a2a.Evidence{
		Ran:    ds.task.Description,
		Output: result.AssistantOutput,
	}
	if runErr != nil {
		ev.ExitCode = 1
		ev.Output = runErr.Error()
	}

	msg := a2a.NewEvidenceMessage(a2a.RoleAgent, ds.task.ID, planID, ev)
	msgJSON, err := jsonMarshalMessage(msg)
	if err != nil {
		return fmt.Errorf("orch: marshal evidence message: %w", err)
	}
	if err := o.store.AppendMessage(ctx, store.AppendMessageOpts{
		WorkerID:    workerID,
		PlanID:      planID,
		TaskID:      ds.task.ID,
		Role:        string(a2a.RoleAgent),
		ContentJSON: msgJSON,
	}); err != nil {
		return fmt.Errorf("orch: append evidence message: %w", err)
	}

	if runErr != nil {
		if _, err := o.store.MarkFailed(ctx, planID, ds.task.ID, sessionID, runErr.Error(), 3); err != nil {
			return fmt.Errorf("orch: mark failed after run error: %w", err)
		}
		_ = o.store.ClearWorkerTask(ctx, workerID, "crashed")
		return nil
	}

	if _, err := o.VerifyAndComplete(ctx, planID, ds.task.ID, ev); err != nil {
		return fmt.Errorf("orch: verify and complete: %w", err)
	}
	_ = o.store.ClearWorkerTask(ctx, workerID, "idle")
	return nil
}
