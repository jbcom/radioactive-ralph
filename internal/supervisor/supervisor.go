package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

// reaperInterval is how often the supervisor sweeps the store for stale
// worker/session claims (store.ReclaimStale) and refreshes its own session
// heartbeat. Kept short relative to the reaper's own staleness window so a
// crashed worker's task comes back to "ready" within a few ticks rather
// than sitting claimed indefinitely.
const reaperInterval = 15 * time.Second

// staleAfter is the heartbeat age past which store.ReclaimStale treats a
// worker/session claim as abandoned. Several multiples of reaperInterval
// so a merely-slow tick never gets mistaken for a crash.
const staleAfter = 90 * time.Second

// Options configures a Supervisor run.
type Options struct {
	// RuntimeDir is the XDG-level directory the supervisor socket,
	// heartbeat, and PID lock live under (spec §5c). Working directory is
	// irrelevant to the supervisor (spec §4) — this is the one path that
	// matters.
	RuntimeDir string

	// Store is the already-open user-level database (spec §6). Supervisor
	// does not open it itself so callers can inject a test double or a
	// store opened with a fake clock.
	Store *store.Store

	// Orchestrator dispatches plan/task work (Phase 6). Optional: nil
	// defaults to orch.New(Store) — a real provider.NewRunner-backed
	// orchestrator. Tests inject one built with a fake RunnerFactory so
	// HandleEnqueue's dispatch wiring can be proven without a real
	// provider CLI.
	Orchestrator *orch.Orchestrator

	// Logger receives lifecycle messages. Optional.
	Logger func(msg string, args ...any)
}

// Supervisor is the small, boring control-plane process described in spec
// §4/§13: pty ownership + IPC + store + reaper, PLUS (as of Phase 6c) real
// plan dispatch: HandleEnqueue drives internal/orch's DispatchNext instead
// of returning "not implemented". Orch itself — via the provider runners
// it dispatches onto internal/agent — owns every agent subprocess's
// lifetime (start, watchdog supervision, kill), so the supervisor holds no
// separate pty-tracking map of its own; there is nothing left for the
// supervisor to additionally track or drain at shutdown.
type Supervisor struct {
	opts      Options
	store     *store.Store
	orch      *orch.Orchestrator
	server    *ipc.Server
	listener  *Listener
	sessionID string

	startedAt time.Time
	log       func(msg string, args ...any)

	// dispatchMu serializes dispatchActivePlans so the periodic tick (Run's
	// goroutine) and HandleEnqueue (an IPC per-connection handler goroutine)
	// never drive the shared orchestrator's DispatchNext concurrently.
	dispatchMu sync.Mutex
	// dispatchCursor rotates the starting offset across dispatch passes so
	// active plans ranked beyond maxEnqueueDispatchPlans aren't starved by a
	// stable ordering that always dispatches the same head of the list.
	dispatchCursor int

	stopCh   chan struct{}
	stopOnce *sync.Once
}

// Run acquires the supervisor socket (failing with ErrSupervisorRunning if
// another instance already holds it), registers a supervisor session in
// the store, serves IPC until ctx is cancelled or a client sends CmdStop,
// and then shuts down cleanly: any in-flight dispatch is bounded by orch's
// own watchdog config, the IPC server is stopped, the socket released, the
// session closed.
func Run(ctx context.Context, opts Options) error {
	if opts.RuntimeDir == "" {
		return fmt.Errorf("supervisor: RuntimeDir required")
	}
	if opts.Store == nil {
		return fmt.Errorf("supervisor: Store required")
	}
	logf := opts.Logger
	if logf == nil {
		logf = func(string, ...any) {}
	}

	listener, err := Acquire(opts.RuntimeDir)
	if err != nil {
		return err
	}

	orchestrator := opts.Orchestrator
	if orchestrator == nil {
		orchestrator = orch.New(opts.Store)
	}

	sup := &Supervisor{
		opts:      opts,
		store:     opts.Store,
		orch:      orchestrator,
		listener:  listener,
		startedAt: time.Now(),
		log:       logf,
		stopCh:    make(chan struct{}),
		stopOnce:  &sync.Once{},
	}

	sessionID, err := sup.store.CreateSession(ctx, store.SessionOpts{
		Role: "supervisor",
		PID:  os.Getpid(),
		Host: hostname(),
	})
	if err != nil {
		_ = listener.Release()
		return fmt.Errorf("supervisor: create session: %w", err)
	}
	sup.sessionID = sessionID

	server, err := ipc.NewServer(ipc.ServerOptions{
		SocketPath:    listener.SocketPath,
		HeartbeatPath: listener.HeartbeatPath,
		Handler:       sup,
	})
	if err != nil {
		_ = sup.store.CloseSession(ctx, sessionID)
		_ = listener.Release()
		return fmt.Errorf("supervisor: new ipc server: %w", err)
	}
	sup.server = server

	if err := server.Start(); err != nil {
		_ = sup.store.CloseSession(ctx, sessionID)
		_ = listener.Release()
		return fmt.Errorf("supervisor: start ipc server: %w", err)
	}
	logf("supervisor listening", "socket", listener.SocketPath, "pid", os.Getpid())

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Async dispatch goroutines must run under the supervisor's run context, not
	// the per-request IPC context that drives a given DispatchNext (that one is
	// cancelled when its request handler returns). Cancelling runCtx on shutdown
	// then aborts every in-flight provider turn so the drain is bounded.
	sup.orch.SetBaseContext(runCtx)

	ticker := time.NewTicker(reaperInterval)
	defer ticker.Stop()

runLoop:
	for {
		select {
		case <-runCtx.Done():
			break runLoop
		case <-sup.stopCh:
			break runLoop
		case <-ticker.C:
			sup.tick(runCtx)
		}
	}

	logf("supervisor shutting down")
	// Cancel the run context BEFORE draining: dispatch now runs each provider
	// turn in its own goroutine (the never-block invariant), so cancellation
	// aborts the in-flight runner.Run subprocesses (via the orchestrator's
	// worker-cancel registry) and makes the drain in shutdown bounded rather than
	// a wait of up to StallTimeout per in-flight worker.
	cancel()
	return sup.shutdown(context.Background())
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// tick runs one periodic pass: reclaim stale worker/session claims, refresh
// this supervisor's own session heartbeat, and DRIVE DISPATCH for every
// active plan store-wide. The dispatch pass is what makes an active plan
// progress on its own — without it, DispatchNext would only ever run in
// response to an explicit HandleEnqueue IPC call, and the periodic loop
// would be a reaper-only heartbeat with no way to actually advance work.
func (s *Supervisor) tick(ctx context.Context) {
	if _, err := s.store.ReclaimStale(ctx, staleAfter); err != nil {
		s.log("reaper tick failed", "err", err)
	}
	if err := s.store.HeartbeatSession(ctx, s.sessionID); err != nil {
		s.log("session heartbeat failed", "err", err)
	}
	// The tick is a best-effort autonomous heartbeat: a transient
	// list-plans/DB error must not tear the loop down, so it is logged and
	// swallowed here (HandleEnqueue, an interactive IPC call, surfaces the
	// same error to its caller instead).
	if n, err := s.dispatchActivePlans(ctx); err != nil {
		s.log("dispatch tick failed", "err", err)
	} else if n > 0 {
		s.log("dispatched ready steps", "count", n)
	}
}

// dispatchActivePlans runs one DispatchNext pass against every active/paused
// plan store-wide (bounded by maxEnqueueDispatchPlans), returning the total
// number of steps dispatched. Shared by the periodic tick (autonomous
// progress) and HandleEnqueue (explicit wake). The supervisor is
// project-agnostic — it has no notion of "the current project" — so this
// scans all projects' active work, not just one.
//
// A ListPlans failure is returned as an error (callers decide whether to
// surface or swallow it). A per-plan DispatchNext failure, by contrast, is
// logged and skipped: one bad plan must not stop the others from advancing.
func (s *Supervisor) dispatchActivePlans(ctx context.Context) (int, error) {
	// Serialize: the tick and HandleEnqueue must not run DispatchNext against
	// the shared orchestrator concurrently.
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()

	// Only ACTIVE plans are dispatched. ListPlans' default (nil) filter returns
	// active AND paused, so passing it here would let the periodic tick keep
	// dispatching ready steps from a plan an operator just paused via the drive
	// API — the pause control would be a no-op. Filter to active explicitly so
	// pause actually halts progress. (Paused plans remain visible to read
	// clients through their own ListPlans calls; they are only excluded here,
	// at the dispatch gate.)
	plans, err := s.store.ListPlans(ctx, "", []store.PlanStatus{store.PlanStatusActive})
	if err != nil {
		return 0, fmt.Errorf("supervisor: list active plans: %w", err)
	}
	total := 0
	// Dispatch at most maxEnqueueDispatchPlans plans per pass, but ROTATE the
	// starting offset each pass so that when more than that many plans are
	// active, plans ranked beyond the cap still get their turn instead of
	// being permanently starved by a stable ordering.
	limit := len(plans)
	if limit > maxEnqueueDispatchPlans {
		limit = maxEnqueueDispatchPlans
	}
	start := 0
	if len(plans) > 0 {
		start = s.dispatchCursor % len(plans)
	}
	for i := 0; i < limit; i++ {
		p := plans[(start+i)%len(plans)]
		n, err := s.orch.DispatchNext(ctx, p.ProjectID, p.ID)
		if err != nil {
			s.log("dispatch failed", "plan", p.ID, "err", err)
			continue
		}
		total += n
	}
	// Advance the cursor past the window we just covered so the next pass
	// starts where this one left off.
	if len(plans) > 0 {
		s.dispatchCursor = (start + limit) % len(plans)
	}
	return total, nil
}

// shutdown stops the IPC server (which also unlinks the socket file),
// closes the supervisor's session row, and releases the PID lock. There is
// no separate agent-draining step: every agent subprocess dispatched via
// s.orch is owned end-to-end (started, watchdog-supervised, killed) by the
// provider runner orch dispatched it through — see Supervisor's doc
// comment — so shutdown has nothing of its own left to kill. Errors are
// collected and joined so one failure doesn't skip the rest of teardown.
func (s *Supervisor) shutdown(ctx context.Context) error {
	var errs []error
	if err := s.server.Stop(); err != nil {
		errs = append(errs, fmt.Errorf("stop ipc server: %w", err))
	}
	// Drain in-flight dispatched worker goroutines (provider turn + verify) so
	// their store writes land before we close the session/DB. The run context was
	// cancelled just before this call, so each in-flight turn is already aborting;
	// this wait is bounded.
	s.orch.Wait()
	if err := s.store.CloseSession(ctx, s.sessionID); err != nil {
		errs = append(errs, fmt.Errorf("close session: %w", err))
	}
	if err := s.listener.Release(); err != nil {
		errs = append(errs, fmt.Errorf("release listener: %w", err))
	}

	if len(errs) == 0 {
		return nil
	}
	joined := errs[0]
	for _, e := range errs[1:] {
		joined = fmt.Errorf("%w; %w", joined, e)
	}
	return joined
}

// --- ipc.Handler ---

// HandleStatus reports supervisor-level liveness. ActiveWorkers and the
// per-worker detail are sourced from the store's real worker rows
// (store.ListRunningWorkers) rather than an in-process map: no in-process
// structure could ever reflect this anyway, since agent subprocess lifetime is
// fully owned by whichever provider runner orch dispatched, not by the
// supervisor itself. A query failure degrades to an empty list / 0 count rather
// than failing the whole status reply — a transient error should never make
// `status` itself fail.
func (s *Supervisor) HandleStatus(ctx context.Context) (ipc.StatusReply, error) {
	// Source both the count AND the per-worker detail from one store read so a
	// client (the GUI) can name a specific worker to kill. A query failure
	// degrades to an empty list / 0 count rather than failing the whole status
	// reply — a transient error should never make `status` itself fail.
	running, err := s.store.ListRunningWorkers(ctx)
	if err != nil {
		s.log("list running workers failed", "err", err)
		running = nil
	}
	workers := make([]ipc.WorkerSummary, 0, len(running))
	for _, w := range running {
		workers = append(workers, ipc.WorkerSummary{
			WorkerID: w.ID,
			PlanID:   w.PlanID,
			TaskID:   w.TaskID,
			Provider: w.Provider,
		})
	}

	// Plan/task aggregate counters. Same degrade-not-fail policy: a failed
	// count query leaves the counters at zero rather than failing `status`.
	counts, err := s.store.StatusCounts(ctx)
	if err != nil {
		s.log("status counts failed", "err", err)
		counts = store.StatusCounts{}
	}

	return ipc.StatusReply{
		ProtoVersion:  ipc.ProtoVersion, // advertise the drive-surface version
		PID:           os.Getpid(),
		Uptime:        time.Since(s.startedAt),
		ActiveWorkers: len(running),
		Workers:       workers,
		ActivePlans:   counts.ActivePlans,
		ReadyTasks:    counts.Ready,
		RunningTasks:  counts.Running,
		ApprovalTasks: counts.Approval,
		BlockedTasks:  counts.Blocked,
		FailedTasks:   counts.Failed,
	}, nil
}

// HandleEnqueue drives one real dispatch pass via internal/orch instead of
// returning "not implemented": it lists every currently active/paused plan
// store-wide (spec: the supervisor is project-agnostic — it has no notion
// of "the current project", so this checks every project's active work,
// not just one) and calls DispatchNext on each, in plan order, until
// either every plan has been tried or maxEnqueueDispatchPlans is reached
// (a bound so one enqueue call can never scan an unbounded number of
// plans). It is NOT a blocking wait for any of that work to finish —
// DispatchNext itself is synchronous per dispatched step but bounded by
// orch's own watchdog config (agent.WatchdogConfig.StallTimeout), so a
// stalled/prompting provider still returns (killed) rather than hanging
// this IPC call.
//
// args.Description/args.TaskID name the work the caller wanted enqueued,
// but a store task cannot be created without a plan_id (tasks.plan_id is a
// NOT NULL foreign key) and EnqueueArgs carries no plan reference — so
// HandleEnqueue's job today is exactly "wake up dispatch for whatever is
// already ready", the same effect an enqueue is meant to have (make
// already-known work actually run), not "materialize a new ad hoc task
// with no plan to belong to". EnqueueReply.Inserted reports whether
// anything was actually dispatched; TaskID echoes args.TaskID (or, if
// unset, the number of steps dispatched, best-effort) so a caller has some
// return value acknowledging its enqueue signal was acted upon.
func (s *Supervisor) HandleEnqueue(ctx context.Context, args ipc.EnqueueArgs) (ipc.EnqueueReply, error) {
	total, err := s.dispatchActivePlans(ctx)
	if err != nil {
		return ipc.EnqueueReply{}, err
	}

	taskID := args.TaskID
	if taskID == "" {
		taskID = fmt.Sprintf("dispatched=%d", total)
	}
	return ipc.EnqueueReply{TaskID: taskID, Inserted: total > 0}, nil
}

// maxEnqueueDispatchPlans bounds how many active plans a single
// HandleEnqueue call will scan/dispatch against, so one IPC call can never
// iterate an unbounded number of plans store-wide.
const maxEnqueueDispatchPlans = 50

// HandleStop breaks Run's select loop, which triggers shutdown. Graceful
// vs. immediate is not yet differentiated (no in-flight plan work exists
// yet to wait on) — both simply request shutdown.
func (s *Supervisor) HandleStop(_ context.Context, _ ipc.StopArgs) error {
	s.stopOnce.Do(func() { close(s.stopCh) })
	return nil
}

// HandleReloadConfig is a no-op today: config reload semantics belong to
// vconfig's virtual-layer resolution (spec §5a), which this minimal
// supervisor does not yet wire into a running process's live config.
func (s *Supervisor) HandleReloadConfig(_ context.Context) error {
	return nil
}

// attachTailInterval is how often HandleAttach polls the events table for new
// rows. Short enough that a live view feels immediate, long enough that an idle
// stream is nearly free. The events table is the single ordered source of
// truth (see docs/superpowers/specs/2026-07-17-attach-event-stream-design.md);
// tailing it reuses that ordering with no change to any emit site.
const attachTailInterval = 250 * time.Millisecond

// attachTailBatch caps how many events one tail tick emits, so a burst can't
// make a single tick unbounded. The next tick drains the remainder immediately.
const attachTailBatch = 256

// HandleAttach streams the project's events to the client as they are written,
// turning the observe half of the drive+observe API from a stub into a live
// feed. It TAILS the append-only events table: each tick it reads rows with id
// greater than the cursor (scoped to args.ProjectID, including plan-linked
// rows), emits each, and advances the cursor. The cursor starts at
// args.AfterID — the client owns it (it obtains an initial value from
// MaxEventID/backlog), so there is no server-side seed and no lost-event race.
// The loop returns when ctx is cancelled (client disconnect — #165's watcher —
// or supervisor shutdown) or when emit reports the client is gone.
func (s *Supervisor) HandleAttach(ctx context.Context, args ipc.AttachArgs, emit func(json.RawMessage) error) error {
	if args.ProjectID == "" {
		return fmt.Errorf("supervisor: attach requires a project id")
	}
	cursor := args.AfterID

	ticker := time.NewTicker(attachTailInterval)
	defer ticker.Stop()

	for {
		// Drain everything currently past the cursor before waiting again, so a
		// burst larger than one batch doesn't wait a full tick between batches.
		for {
			events, err := s.store.EventsAfter(ctx, args.ProjectID, cursor, attachTailBatch)
			if err != nil {
				// Cancellation is a clean end, not an error to surface.
				if ctx.Err() != nil {
					return nil
				}
				// A PERMANENT store failure (closed DB, corruption, schema error)
				// won't fix itself: retrying every tick would just spin, log
				// forever, and leave the client attached to a permanently stale
				// stream. End the subscription with the error so the client sees
				// the stream close rather than hang silently. Only a TRANSIENT
				// error (momentary pool contention) is retried: log, stop
				// draining, wait for the next tick with the cursor unchanged so
				// nothing is skipped.
				if !isTransientStoreErr(err) {
					s.log("attach tail: permanent store error, ending stream", "err", err)
					return fmt.Errorf("supervisor: attach tail: %w", err)
				}
				s.log("attach tail: transient error, retrying next tick", "err", err)
				break
			}
			if len(events) == 0 {
				break
			}
			for _, ev := range events {
				frame, err := json.Marshal(attachEventFrame(ev))
				if err != nil {
					// A single un-marshalable row must not wedge the stream;
					// skip it but still advance past it so the tail progresses.
					s.log("attach tail: marshal event", "event_id", ev.ID, "err", err)
					cursor = ev.ID
					continue
				}
				if err := emit(frame); err != nil {
					// Client gone / write deadline — end the subscription. This
					// is the emit contract the IPC server relies on.
					return nil
				}
				cursor = ev.ID
			}
			if len(events) < attachTailBatch {
				break // fully drained
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// isTransientStoreErr reports whether a store read error is worth retrying on
// the next tail tick (momentary contention) versus a permanent failure that
// should end the stream. SQLite surfaces contention as SQLITE_BUSY /
// SQLITE_LOCKED — recoverable. A closed DB / done connection / done transaction
// (sql.ErrConnDone, sql.ErrTxDone) or anything else is treated as permanent, so
// the tail ends rather than spinning forever against a broken store. Context
// cancellation is handled by the caller before this is consulted.
func isTransientStoreErr(err error) bool {
	if errors.Is(err, sql.ErrConnDone) || errors.Is(err, sql.ErrTxDone) {
		return false
	}
	// modernc.org/sqlite reports lock contention via an error whose message
	// carries the SQLITE_BUSY/SQLITE_LOCKED token; match on that rather than the
	// driver's un-exported error type so we don't couple to its internals.
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") ||
		strings.Contains(msg, "SQLITE_LOCKED") ||
		strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked")
}

// attachEventFrame maps a store event row to the public wire shape. Payload is
// the row's payload_json passed through verbatim as raw JSON — the stream stays
// kind-agnostic so a new event kind needs no transport change.
func attachEventFrame(ev store.Event) ipc.AttachEvent {
	var payload json.RawMessage
	if ev.PayloadJSON != "" {
		payload = json.RawMessage(ev.PayloadJSON)
	}
	return ipc.AttachEvent{
		ID:         ev.ID,
		Kind:       ev.Kind,
		Stream:     ev.Stream,
		PlanID:     ev.PlanID,
		TaskID:     ev.TaskID,
		Actor:      ev.Actor,
		Payload:    payload,
		OccurredAt: ev.OccurredAt,
	}
}
