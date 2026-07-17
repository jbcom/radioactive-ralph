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
	"errors"
	"fmt"
	"strings"
	"sync"
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
	// orchSessionMu guards the lazy init: DispatchNext is driven concurrently
	// from the supervisor's periodic tick AND its IPC HandleEnqueue handler,
	// so two goroutines could otherwise race to create the session row.
	orchSessionMu sync.Mutex
	orchSessionID string

	// runningWorkers maps a live worker id to the CancelFunc of its in-flight
	// runner.Run context, so an operator/GUI worker-kill can actually cancel the
	// provider subprocess (exec.CommandContext kills the process tree on ctx
	// cancel) rather than only doing store bookkeeping and leaving the process
	// running against the checkout. An entry exists only for the duration of a
	// worker's agent turn; dispatchWorker/dispatchFanoutGroup register on start
	// and deregister the instant Run returns. runningWorkersMu guards the map,
	// which is touched from every concurrent dispatch and from KillWorker.
	runningWorkersMu sync.Mutex
	runningWorkers   map[string]context.CancelFunc

	// capInFlightMu guards capInFlight: the count of dispatched-but-not-yet-
	// recorded provider turns per CAPPED provider. Async dispatch made
	// checkSpendCap racy — N concurrent ready steps could each observe the same
	// persisted balance below the cap and all launch, overspending by N turns.
	// A per-turn cost isn't known until the turn finishes, so a precise
	// reservation is impossible; instead we serialize a capped provider to ONE
	// in-flight turn at a time (checkSpendCap refuses a second), bounding any
	// overshoot to a single turn's cost — the unavoidable minimum. Uncapped
	// providers are never tracked here (no lookup, no contention).
	capInFlightMu sync.Mutex
	capInFlight   map[string]int

	// Async dispatch (the never-block invariant): dispatchWorker runs the provider
	// agent turn, which can block for up to watchdogConfig.StallTimeout. It must
	// NOT run inline under the supervisor's dispatchMu, or a slow turn wedges the
	// periodic tick, HandleEnqueue, and the reaper. So DispatchNext launches each
	// worker in its own goroutine, tracked by inflight (for shutdown drain) and
	// bounded by the dispatchSem semaphore (to preserve the maxParallel cap that
	// synchronous dispatch used to provide by blocking). See the design doc:
	// docs/superpowers/specs/2026-07-17-async-dispatch-never-block-design.md.
	inflight    sync.WaitGroup
	dispatchSem chan struct{} // buffered to maxParallel; nil disables the bound

	// heartbeatInterval overrides workerHeartbeatInterval for tests (so a test
	// can prove the running-worker heartbeat fires without waiting 20s). Zero uses
	// the production constant.
	heartbeatInterval time.Duration

	// baseCtx is the context the ASYNC dispatch goroutines run under — the
	// provider turn, store writes, and verification. It must outlive the
	// per-dispatch caller's ctx: DispatchNext is driven from HandleEnqueue, whose
	// ctx is the IPC request context, cancelled the instant the request handler
	// returns. If the goroutine used that ctx, runner.Run and every store write
	// would run against a cancelled context and fail silently. So the goroutine
	// derives its context from baseCtx (the supervisor's run context, via
	// WithBaseContext) instead. Defaults to context.Background().
	// baseCtxMu guards it: SetBaseContext writes it from Run while DispatchNext
	// reads it concurrently from the tick / HandleEnqueue.
	baseCtxMu sync.RWMutex
	baseCtx   context.Context
}

// workerHeartbeatInterval is how often a running worker's store heartbeat is
// refreshed during its (now asynchronous) provider turn. It MUST be well below
// the supervisor's staleAfter (90s) or the reaper would reclaim a healthy
// long-running worker mid-turn — a regression the async dispatch introduced,
// since the reaper tick now runs concurrently with provider turns instead of
// being blocked behind them. 20s gives several beats within the staleness
// window.
const workerHeartbeatInterval = 20 * time.Second

// runWithHeartbeat runs fn (a provider turn) while periodically refreshing
// workerID's store heartbeat, so the supervisor's reaper does not mistake a
// legitimately long-running worker for a crashed one and reclaim its task
// out from under it. The heartbeat goroutine stops the instant fn returns.
// hbCtx bounds the heartbeat loop (the dispatch base context); heartbeat write
// errors are ignored — a missed beat is self-correcting on the next tick, and a
// heartbeat failure must never fail the turn.
func (o *Orchestrator) runWithHeartbeat(hbCtx context.Context, workerID string, fn func() (provider.Result, error)) (provider.Result, error) {
	interval := workerHeartbeatInterval
	if o.heartbeatInterval > 0 {
		interval = o.heartbeatInterval // test override
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-hbCtx.Done():
				return
			case <-t.C:
				_ = o.store.HeartbeatWorker(hbCtx, workerID)
			}
		}
	}()
	result, err := fn()
	close(done)
	wg.Wait()
	return result, err
}

// registerWorker records cancel as the way to abort workerID's in-flight run
// and returns a deregister func to call (via defer) the moment the run returns.
// Keeping the window tight — only around runner.Run — means KillWorker can never
// cancel post-run store writes or verification, which use the parent ctx.
func (o *Orchestrator) registerWorker(workerID string, cancel context.CancelFunc) (deregister func()) {
	o.runningWorkersMu.Lock()
	if o.runningWorkers == nil {
		o.runningWorkers = make(map[string]context.CancelFunc)
	}
	o.runningWorkers[workerID] = cancel
	o.runningWorkersMu.Unlock()
	return func() {
		o.runningWorkersMu.Lock()
		delete(o.runningWorkers, workerID)
		o.runningWorkersMu.Unlock()
	}
}

// KillWorker cancels the in-flight agent run for workerID, if one is currently
// registered, and reports whether it did. This is the process half of a
// worker-kill: exec.CommandContext propagates the cancellation to the provider
// subprocess so it stops consuming tokens and touching the checkout. The store
// half (requeue the task, terminate the row) is store.ReclaimWorker; the
// supervisor's HandleWorkerKill invokes both. A false return means no run was
// live under that id (already finished, or a fan-out member id) — harmless, and
// the store side still runs.
func (o *Orchestrator) KillWorker(workerID string) bool {
	o.runningWorkersMu.Lock()
	cancel, ok := o.runningWorkers[workerID]
	o.runningWorkersMu.Unlock()
	if ok {
		cancel()
	}
	return ok
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

// withHeartbeatInterval overrides the running-worker heartbeat cadence. Test-only
// (unexported) so a test can observe a heartbeat within milliseconds instead of
// the 20s production interval.
func withHeartbeatInterval(d time.Duration) Option {
	return func(o *Orchestrator) { o.heartbeatInterval = d }
}

// WithBaseContext sets the long-lived context the async dispatch goroutines run
// under (provider turn + store writes + verification). The supervisor passes its
// run context so dispatched work survives past the per-request IPC context that
// drove it. A nil ctx is ignored (keeps the Background default).
func WithBaseContext(ctx context.Context) Option {
	return func(o *Orchestrator) {
		if ctx != nil {
			o.baseCtx = ctx
		}
	}
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
		baseCtx:         context.Background(),
	}
	for _, opt := range opts {
		opt(o)
	}
	// Size the dispatch semaphore from the resolved maxParallel so async dispatch
	// keeps the same store-wide concurrency bound synchronous dispatch enforced by
	// blocking. maxParallel <= 0 means "unbounded" (nil sem = no throttle).
	if o.maxParallel > 0 {
		o.dispatchSem = make(chan struct{}, o.maxParallel)
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

// fanoutScopedContext is the plan-scoped context handed to a SINGLE
// fan-out-capable worker standing in for an entire parallel step-group
// (see dispatchFanoutGroup): every ready step's text/detail is listed
// together (each tagged by its stable task id) so the worker knows to fan
// the work out itself, but the prompt still never includes the whole plan
// document — only this one group, exactly like scopedContext for the
// single-step case.
type fanoutScopedContext struct {
	PlanTitle    string
	GroupHeading string
	Steps        []dispatchedStep
}

// prompt renders the fan-out group's scoped context as the worker's user
// prompt: the plan/group heading, then every ready step in the group
// labeled by its stable task id, with an explicit instruction that the
// worker's own CLI/API is expected to fan this list out across its native
// subagent/parallelism support rather than handling steps one at a time.
func (c fanoutScopedContext) prompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Plan: %s\n", c.PlanTitle)
	fmt.Fprintf(&b, "Group: %s\n\n", c.GroupHeading)
	b.WriteString("This provider supports native fan-out. The following steps in this group are independent and ready to run in parallel — use your own subagent/parallel-workflow support to work them concurrently, and report on each one individually by its task id.\n\n")
	for _, ds := range c.Steps {
		fmt.Fprintf(&b, "[%s] %s", ds.task.ID, ds.step.Text)
		if ds.step.Detail != "" {
			fmt.Fprintf(&b, "\n%s", ds.step.Detail)
		}
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
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

	// Resolve the project's checkout dir ONCE for this dispatch pass. Every
	// worker for this plan runs in the project's own tree, not the
	// supervisor's cwd (§4: supervisor working directory is irrelevant).
	projectDir, err := o.projectDirFor(ctx, planID)
	if err != nil {
		return 0, err
	}

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

	// Fan-out delegation: when the ready group is Parallel AND the binding
	// resolved for it declares NativeFanout, one fan-out-capable worker
	// takes the WHOLE group in a single invocation (the CLI/API manages
	// its own sub-agents/parallelism internally) instead of Ralph spawning
	// N Ralph-managed workers. This must be decided BEFORE the per-step
	// dispatch loop below, since it dispatches differently (one worker
	// claiming every ready task in the group, one provider turn, one
	// evidence submission mapped back onto every task) rather than one
	// worker per step.
	if parallel && limit > 1 {
		binding, err := o.resolveBinding(ctx, projectID, parallel)
		if err != nil {
			return dispatched, fmt.Errorf("orch: resolve binding: %w", err)
		}
		if binding.Config.NativeFanout {
			// A fan-out group is one worker / one provider turn, so it takes ONE
			// dispatch slot. If the pipeline is full there's no capacity now —
			// return without dispatching; the next pass retries.
			if !o.acquireDispatchSlot() {
				return dispatched, nil
			}
			slotReleased := false
			releaseSlot := func() {
				if !slotReleased {
					o.releaseDispatchSlot()
					slotReleased = true
				}
			}
			n, err := o.dispatchFanoutGroup(ctx, projectID, projectDir, planID, parsedPlan, storedPlan.Title, groupHeading, binding, readySteps[:limit], refs[:limit], releaseSlot)
			if err != nil {
				// dispatchFanoutGroup only returns an error on a synchronous
				// pre-launch failure (claim/spend); the async turn was never
				// started, so release the slot here.
				releaseSlot()
				return dispatched, err
			}
			return n, nil
		}
	}

	for i := 0; i < limit; i++ {
		// Try to acquire a dispatch slot BEFORE claiming a task. If the semaphore
		// is full, there's no free capacity right now: stop the pass rather than
		// claim a task we can't run or block the lock. The next tick / enqueue
		// resumes from here. (nil sem = unbounded; acquire always succeeds.)
		if !o.acquireDispatchSlot() {
			break
		}
		releaseSlot := true // release inline on any pre-launch early exit below
		spendProvider := "" // set once checkSpendCap reserves; released with the slot
		release := func() {
			if releaseSlot {
				o.releaseDispatchSlot()
				releaseSlot = false
			}
			if spendProvider != "" {
				o.releaseSpendReservation(projectID, spendProvider)
				spendProvider = ""
			}
		}

		binding, err := o.resolveBinding(ctx, projectID, parallel)
		if err != nil {
			release()
			return dispatched, fmt.Errorf("orch: resolve binding: %w", err)
		}

		if err := o.checkSpendCap(ctx, projectID, binding.Name); err != nil {
			// Spend-cap refusal is not a fatal DispatchNext error: other
			// steps (possibly on an uncapped provider) may still be
			// dispatchable. Record the refusal and move to the next ready
			// step.
			release()
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
		// checkSpendCap reserved an in-flight slot for a capped provider; record
		// it so release() frees it on any pre-launch early exit below, and so
		// ownership can transfer to the async goroutine on a successful launch.
		spendProvider = binding.Name

		sessionID, workerID, err := o.spawnWorkerRows(ctx, binding)
		if err != nil {
			release()
			return dispatched, err
		}

		ds, err := o.claimStepTask(ctx, planID, parsedPlan, refs[i], readySteps[i], sessionID, workerID)
		if err != nil {
			// Release the worker row we just spawned — otherwise it leaks in
			// 'running' with no task (CreateWorker hardcodes status='running').
			release()
			_ = o.store.ClearWorkerTask(ctx, workerID, "crashed")
			return dispatched, err
		}
		if ds == nil {
			// Nothing ready to claim (lost the race to another claimer, or
			// the task doesn't exist yet under this plan — see
			// claimStepTask). Not an error; just nothing to dispatch here.
			// Release the worker row we just spawned since it will never
			// be assigned a task.
			release()
			_ = o.store.ClearWorkerTask(ctx, workerID, "idle")
			continue
		}
		if err := o.store.SetWorkerTask(ctx, workerID, planID, ds.task.ID); err != nil {
			release()
			return dispatched, fmt.Errorf("orch: set worker task: %w", err)
		}

		scoped := scopedContext{
			PlanTitle:    planTitle(parsedPlan, storedPlan.Title),
			GroupHeading: groupHeading,
			StepText:     ds.step.Text,
			StepDetail:   ds.step.Detail,
		}

		// Launch the provider turn asynchronously: the claim above (fast store
		// I/O) has happened synchronously under the caller's dispatchMu, but the
		// slow agent turn + verify must run off the lock. The slot and the
		// inflight WaitGroup are released when the goroutine finishes; ownership of
		// releaseSlot transfers to the goroutine here, so the loop's inline release
		// no longer fires for this iteration.
		releaseSlot = false
		reservedProvider := spendProvider // transfer the reservation to the goroutine
		spendProvider = ""                // loop's release() must not also free it
		o.inflight.Add(1)
		// Run under baseCtx, NOT the caller's ctx: HandleEnqueue's ctx is the IPC
		// request context, cancelled the moment the request handler returns, which
		// would kill this goroutine's run + store writes.
		runCtx := o.dispatchBaseCtx()
		go func() {
			defer o.inflight.Done()
			defer o.releaseDispatchSlot()
			// Release the spend reservation once the turn's usage is recorded
			// (dispatchWorker records it before returning), freeing the capped
			// provider for its next turn.
			defer o.releaseSpendReservation(projectID, reservedProvider)
			if err := o.dispatchWorker(runCtx, projectID, projectDir, planID, sessionID, workerID, binding, ds, scoped); err != nil {
				// Async: no caller to return to. A dispatch error for one worker
				// must never abort the pass or wedge the supervisor — log it as a
				// store event (same shape as spend-cap refusal) and move on.
				_ = o.store.Emit(runCtx, store.EmitOpts{
					ProjectID:   projectID,
					PlanID:      planID,
					Kind:        "worker.dispatch_error",
					Stream:      "service",
					PayloadJSON: mustPayloadJSON(store.EventPayload{Reason: err.Error()}),
				})
			}
		}()
		dispatched++
	}

	return dispatched, nil
}

// acquireDispatchSlot try-acquires one dispatch slot without blocking, returning
// true on success. A nil semaphore (maxParallel <= 0) means unbounded — always
// true. Non-blocking so a full pipeline stops the dispatch pass instead of
// blocking the caller's dispatchMu.
func (o *Orchestrator) acquireDispatchSlot() bool {
	if o.dispatchSem == nil {
		return true
	}
	select {
	case o.dispatchSem <- struct{}{}:
		return true
	default:
		return false
	}
}

// releaseDispatchSlot returns one dispatch slot. Safe to call once per successful
// acquire; a no-op when the semaphore is nil.
func (o *Orchestrator) releaseDispatchSlot() {
	if o.dispatchSem == nil {
		return
	}
	<-o.dispatchSem
}

// SetBaseContext sets the long-lived context async dispatch goroutines run
// under. The supervisor calls this once at the top of Run with its run context —
// the orchestrator is constructed before that context exists, so it can't be a
// construction option there. Must be called before the first DispatchNext. A nil
// ctx is ignored.
func (o *Orchestrator) SetBaseContext(ctx context.Context) {
	if ctx == nil {
		return
	}
	o.baseCtxMu.Lock()
	o.baseCtx = ctx
	o.baseCtxMu.Unlock()
}

// dispatchBaseCtx returns the context async dispatch goroutines run under.
func (o *Orchestrator) dispatchBaseCtx() context.Context {
	o.baseCtxMu.RLock()
	defer o.baseCtxMu.RUnlock()
	return o.baseCtx
}

// Wait blocks until every in-flight dispatched worker goroutine has finished
// (its provider turn + verification returned). The supervisor calls this during
// shutdown AFTER cancelling the run context — cancellation aborts the in-flight
// runner.Run subprocesses via the runningWorkers registry, so this drain is
// bounded, not a hang. Tests also call it to observe dispatch results
// deterministically now that dispatch is asynchronous.
func (o *Orchestrator) Wait() {
	o.inflight.Wait()
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
	o.orchSessionMu.Lock()
	defer o.orchSessionMu.Unlock()
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
		// Only the benign "another dispatcher already created this row" race
		// (ErrDuplicateTask) is tolerable here — the loser falls through to
		// the race-safe claim below. A REAL insert failure (disk full, DB
		// busy, I/O) must surface, not be swallowed into a silent stall.
		if err := o.store.CreateTask(ctx, store.CreateTaskOpts{
			PlanID:         planID,
			ID:             taskID,
			Description:    step.Text,
			AcceptanceJSON: acceptance,
		}); err != nil && !errors.Is(err, store.ErrDuplicateTask) {
			return nil, fmt.Errorf("orch: materialize task %s: %w", taskID, err)
		}
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
func (o *Orchestrator) dispatchWorker(ctx context.Context, projectID, projectDir, planID, sessionID, workerID string, binding provider.Binding, ds *dispatchedStep, scoped scopedContext) error {
	runner, err := o.newRunner(binding)
	if err != nil {
		return fmt.Errorf("orch: resolve runner for %q: %w", binding.Name, err)
	}

	// The stall timeout bounds ONLY the agent turn (runner.Run), not the
	// post-run store writes and verification below. Threading the shrinking
	// timeout ctx into VerifyAndComplete made a near-timeout run's acceptance
	// re-check (which re-runs real shell commands) fail spuriously against an
	// already-expired deadline. Use runCtx for Run; keep the parent ctx after.
	req := provider.Request{
		WorkingDir: projectDir,
		UserPrompt: scoped.prompt(),
	}

	// The stall timeout bounds ONLY the agent turn: cancel it the instant Run
	// returns (not at function end) so the timeout resources aren't held
	// through the slower post-run store writes + VerifyAndComplete, which use
	// the parent ctx.
	result, runErr := o.runWithHeartbeat(ctx, workerID, func() (provider.Result, error) {
		// A cancelable run context registered under workerID so an operator/GUI
		// worker-kill can abort this provider turn (see KillWorker). The stall
		// timeout, when set, layers on top of the same cancelable context.
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		if o.watchdogConfig.StallTimeout > 0 {
			var tcancel context.CancelFunc
			runCtx, tcancel = context.WithTimeout(runCtx, o.watchdogConfig.StallTimeout)
			defer tcancel()
		}
		deregister := o.registerWorker(workerID, cancel)
		defer deregister()
		return runner.Run(runCtx, binding, req)
	})

	// Post-run store writes (usage, evidence, mark-failed/verify, clear-worker)
	// run under persistCtx, NOT ctx. ctx is the supervisor's run context, which
	// shutdown cancels to abort the in-flight provider turn — but once the turn
	// has produced its result, that result MUST be recorded (spend accounting) and
	// verified, or a nearly-complete turn is silently lost on shutdown and its
	// task stuck 'running'. persistCtx is detached from ctx's cancellation with a
	// bounded timeout so these writes land even as ctx is being torn down.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer persistCancel()

	// Spend is real the moment tokens were billed, independent of whether
	// the work is ultimately accepted by VerifyAndComplete — record it
	// unconditionally so spend-cap accounting stays accurate even for
	// rejected/failed turns.
	if err := o.recordUsage(persistCtx, projectID, workerID, binding.Name, string(req.Model), result.Usage); err != nil {
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
	if err := o.store.AppendMessage(persistCtx, store.AppendMessageOpts{
		WorkerID:    workerID,
		PlanID:      planID,
		TaskID:      ds.task.ID,
		Role:        string(a2a.RoleAgent),
		ContentJSON: msgJSON,
	}); err != nil {
		return fmt.Errorf("orch: append evidence message: %w", err)
	}

	if runErr != nil {
		// A stale failure report (the reaper reclaimed this worker's claim
		// mid-run and possibly reassigned the task) is benign — the current
		// owner keeps its work; we just release this now-defunct worker.
		if _, err := o.store.MarkFailed(persistCtx, planID, ds.task.ID, sessionID, runErr.Error(), 3); err != nil && !errors.Is(err, store.ErrTaskNotOwnedRunning) {
			return fmt.Errorf("orch: mark failed after run error: %w", err)
		}
		_ = o.store.ClearWorkerTask(persistCtx, workerID, "crashed")
		return nil
	}

	if _, err := o.VerifyAndComplete(persistCtx, planID, ds.task.ID, ev); err != nil {
		return fmt.Errorf("orch: verify and complete: %w", err)
	}
	_ = o.store.ClearWorkerTask(persistCtx, workerID, "idle")
	return nil
}

// dispatchFanoutGroup delegates an entire ready parallel step-group to ONE
// fan-out-capable worker instead of Ralph spawning one worker per step (see
// the NativeFanout check in DispatchNext). It claims every step's task
// under a single shared session/worker pair, runs exactly one provider
// turn whose prompt lists the whole group (fanoutScopedContext), and then
// maps that single turn's evidence back onto EVERY task in the group:
// each task is independently verified against its own acceptance criteria
// via VerifyAndComplete (mirroring dispatchWorker's per-step verification,
// just fanned out over N tasks instead of one), so one worker's run can
// still leave some steps done and others rejected/retryable exactly as if
// they'd been dispatched individually.
//
// Admission (spend-cap) is checked once for the whole group, since only
// one provider turn is actually run — a group that can't be admitted
// dispatches nothing and returns 0 rather than partially claiming tasks.
// dispatchFanoutGroup claims an entire ready parallel step-group under one
// worker (fast, synchronous store I/O under the caller's dispatchMu), then
// launches the single fan-out provider turn + verification asynchronously via
// runFanoutGroup. releaseFanoutSlot returns the dispatch slot the caller
// acquired; ownership transfers to the async goroutine on a successful launch,
// and dispatchFanoutGroup releases it inline on any synchronous early exit.
func (o *Orchestrator) dispatchFanoutGroup(ctx context.Context, projectID, projectDir, planID string, parsedPlan *plan.Plan, storeTitle, groupHeading string, binding provider.Binding, steps []plan.Step, refs []plan.StepRef, releaseFanoutSlot func()) (dispatched int, err error) {
	if err := o.checkSpendCap(ctx, projectID, binding.Name); err != nil {
		releaseFanoutSlot()
		_ = o.store.Emit(ctx, store.EmitOpts{
			ProjectID: projectID,
			PlanID:    planID,
			Kind:      "worker.admission_refused",
			Stream:    "service",
			PayloadJSON: mustPayloadJSON(store.EventPayload{
				Reason: err.Error(),
			}),
		})
		return 0, nil
	}
	// checkSpendCap reserved an in-flight slot for a capped provider. Fold its
	// release into releaseFanoutSlot so every synchronous early-exit below frees
	// both; on a successful launch, ownership transfers to the goroutine.
	baseReleaseFanoutSlot := releaseFanoutSlot
	spendReserved := binding.Name
	releaseFanoutSlot = func() {
		baseReleaseFanoutSlot()
		if spendReserved != "" {
			o.releaseSpendReservation(projectID, spendReserved)
			spendReserved = ""
		}
	}

	sessionID, workerID, err := o.spawnWorkerRows(ctx, binding)
	if err != nil {
		releaseFanoutSlot()
		return 0, err
	}

	claimed := make([]*dispatchedStep, 0, len(steps))
	for i, ref := range refs {
		ds, err := o.claimStepTask(ctx, planID, parsedPlan, ref, steps[i], sessionID, workerID)
		if err != nil {
			// A mid-loop claim error would otherwise leave the tasks this
			// worker ALREADY claimed (transitioned to 'running') wedged with
			// no worker to complete them. Release them back to pending so the
			// reaper/next dispatch can pick them up, then fail.
			releaseFanoutSlot()
			o.releaseClaimedTasks(ctx, planID, sessionID, claimed)
			_ = o.store.ClearWorkerTask(ctx, workerID, "crashed")
			return 0, err
		}
		if ds == nil {
			// Lost the claim race on this particular step (another
			// dispatcher got there first) — fine, just fan out over
			// whatever this worker DID manage to claim.
			continue
		}
		claimed = append(claimed, ds)
	}
	if len(claimed) == 0 {
		// Every step in the group was claimed by someone else between
		// DecomposeRefs computing readiness and this call landing its
		// claims. Release the worker row; nothing to dispatch.
		releaseFanoutSlot()
		_ = o.store.ClearWorkerTask(ctx, workerID, "idle")
		return 0, nil
	}
	if err := o.store.SetWorkerTask(ctx, workerID, planID, claimed[0].task.ID); err != nil {
		// Don't leak: the tasks are already claimed ('running') under this worker,
		// so requeue them and release the worker row, mirroring the mid-loop
		// claim-error path above — otherwise they sit wedged until the reaper.
		releaseFanoutSlot()
		o.releaseClaimedTasks(ctx, planID, sessionID, claimed)
		_ = o.store.ClearWorkerTask(ctx, workerID, "crashed")
		return 0, fmt.Errorf("orch: set worker task: %w", err)
	}

	scoped := fanoutScopedContext{
		PlanTitle:    planTitle(parsedPlan, storeTitle),
		GroupHeading: groupHeading,
		Steps:        derefSteps(claimed),
	}

	// The claim phase above is fast store I/O and has run synchronously under the
	// caller's dispatchMu. The slow fan-out turn + per-task verify must run off
	// the lock — launch it asynchronously, bounded and drained the same way the
	// per-step path is. The dispatch slot for a fan-out group was already acquired
	// by the DispatchNext caller; ownership of its release transfers to this
	// goroutine (see the releaseFanoutSlot passed in).
	o.inflight.Add(1)
	// Run under baseCtx, NOT the caller's ctx (which for HandleEnqueue dies when
	// the IPC request returns).
	runCtx := o.dispatchBaseCtx()
	go func() {
		defer o.inflight.Done()
		defer releaseFanoutSlot()
		if err := o.runFanoutGroup(runCtx, projectID, projectDir, planID, groupHeading, binding, sessionID, workerID, claimed, scoped); err != nil {
			_ = o.store.Emit(runCtx, store.EmitOpts{
				ProjectID:   projectID,
				PlanID:      planID,
				Kind:        "worker.dispatch_error",
				Stream:      "service",
				PayloadJSON: mustPayloadJSON(store.EventPayload{Reason: err.Error()}),
			})
		}
	}()
	return len(claimed), nil
}

// runFanoutGroup runs the ONE provider turn for a claimed fan-out group and maps
// its evidence onto every claimed task's verification. Split out of
// dispatchFanoutGroup so the slow turn + verify runs in a goroutine off the
// caller's dispatchMu (the never-block invariant). Errors are returned to the
// launching goroutine, which emits them as a store event.
func (o *Orchestrator) runFanoutGroup(ctx context.Context, projectID, projectDir, planID, groupHeading string, binding provider.Binding, sessionID, workerID string, claimed []*dispatchedStep, scoped fanoutScopedContext) error {
	runner, err := o.newRunner(binding)
	if err != nil {
		return fmt.Errorf("orch: resolve runner for %q: %w", binding.Name, err)
	}

	req := provider.Request{
		WorkingDir: projectDir,
		UserPrompt: scoped.prompt(),
	}
	// Stall timeout bounds ONLY the fan-out turn; cancel it the instant Run
	// returns so it isn't held through the per-task verification below. The run
	// context is also registered under workerID so a worker-kill cancels the
	// whole fan-out turn (see KillWorker).
	result, runErr := o.runWithHeartbeat(ctx, workerID, func() (provider.Result, error) {
		runCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		if o.watchdogConfig.StallTimeout > 0 {
			var tcancel context.CancelFunc
			runCtx, tcancel = context.WithTimeout(runCtx, o.watchdogConfig.StallTimeout)
			defer tcancel()
		}
		deregister := o.registerWorker(workerID, cancel)
		defer deregister()
		return runner.Run(runCtx, binding, req)
	})

	// Post-run writes run under persistCtx (detached from ctx's shutdown
	// cancellation, bounded timeout) so a nearly-complete fan-out turn's evidence
	// + verification still land even as the supervisor tears ctx down. See the
	// same note in dispatchWorker.
	persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer persistCancel()

	// Spend is real the moment tokens were billed, independent of
	// per-task verification outcome — record it once for the group's one
	// provider turn.
	if err := o.recordUsage(persistCtx, projectID, workerID, binding.Name, string(req.Model), result.Usage); err != nil {
		return fmt.Errorf("orch: record usage: %w", err)
	}

	ev := a2a.Evidence{
		Ran:    fmt.Sprintf("fan-out group %q (%d steps)", groupHeading, len(claimed)),
		Output: result.AssistantOutput,
	}
	if runErr != nil {
		ev.ExitCode = 1
		ev.Output = runErr.Error()
	}

	// The single evidence message is logged once per task, since
	// a2a_messages is keyed by task id and each claimed task deserves its
	// own audit trail entry for what evidence it was verified against.
	for _, ds := range claimed {
		msg := a2a.NewEvidenceMessage(a2a.RoleAgent, ds.task.ID, planID, ev)
		msgJSON, err := jsonMarshalMessage(msg)
		if err != nil {
			return fmt.Errorf("orch: marshal evidence message: %w", err)
		}
		if err := o.store.AppendMessage(persistCtx, store.AppendMessageOpts{
			WorkerID:    workerID,
			PlanID:      planID,
			TaskID:      ds.task.ID,
			Role:        string(a2a.RoleAgent),
			ContentJSON: msgJSON,
		}); err != nil {
			return fmt.Errorf("orch: append evidence message: %w", err)
		}
	}

	if runErr != nil {
		for _, ds := range claimed {
			// Benign if the reaper already reclaimed/reassigned this task —
			// don't stomp the new owner (see MarkFailed's owner guard).
			if _, err := o.store.MarkFailed(persistCtx, planID, ds.task.ID, sessionID, runErr.Error(), 3); err != nil && !errors.Is(err, store.ErrTaskNotOwnedRunning) {
				return fmt.Errorf("orch: mark failed after run error: %w", err)
			}
		}
		_ = o.store.ClearWorkerTask(persistCtx, workerID, "crashed")
		return nil
	}

	// Map the ONE turn's evidence back onto EVERY claimed task: each is
	// independently verified against its own acceptance criteria, exactly
	// as dispatchWorker does for a single step. A worker terminating is
	// never completion for any of them — VerifyAndComplete still
	// mechanically re-checks (or falls back to non-empty-evidence
	// judgment) per task.
	// Verify EVERY claimed task even if one errors: an early return would leave
	// the group's remaining tasks wedged in 'running' until the reaper and skip
	// the ClearWorkerTask below (leaking the worker row). Collect the first error
	// and keep going, then always release the worker.
	var verifyErr error
	for _, ds := range claimed {
		if _, err := o.VerifyAndComplete(persistCtx, planID, ds.task.ID, ev); err != nil && verifyErr == nil {
			verifyErr = fmt.Errorf("orch: verify and complete %s: %w", ds.task.ID, err)
		}
	}
	_ = o.store.ClearWorkerTask(persistCtx, workerID, "idle")
	return verifyErr
}

// releaseClaimedTasks requeues tasks a fan-out worker had already claimed
// when a later claim in the same group fails, so they don't sit wedged in
// 'running' with no worker. Uses store.ReleaseClaim (NOT MarkFailed): this
// is a system-level abort, not a task-execution failure, so it must NOT
// charge a retry — otherwise repeated transient claim/materialization
// hiccups could exhaust an otherwise-valid task's budget and terminally fail
// it. Best-effort: individual failures are ignored — the reaper is the
// backstop.
func (o *Orchestrator) releaseClaimedTasks(ctx context.Context, planID, sessionID string, claimed []*dispatchedStep) {
	for _, ds := range claimed {
		_ = o.store.ReleaseClaim(ctx, planID, ds.task.ID, sessionID, "released: fan-out group claim aborted")
	}
}

// derefSteps extracts the dispatchedStep values (not pointers) for
// fanoutScopedContext.Steps, preserving claim order.
func derefSteps(claimed []*dispatchedStep) []dispatchedStep {
	out := make([]dispatchedStep, len(claimed))
	for i, ds := range claimed {
		out[i] = *ds
	}
	return out
}
