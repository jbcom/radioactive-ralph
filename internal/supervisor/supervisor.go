package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

	if err := server.Start(0); err != nil {
		_ = sup.store.CloseSession(ctx, sessionID)
		_ = listener.Release()
		return fmt.Errorf("supervisor: start ipc server: %w", err)
	}
	logf("supervisor listening", "socket", listener.SocketPath, "pid", os.Getpid())

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

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
	plans, err := s.store.ListPlans(ctx, "", nil)
	if err != nil {
		return 0, fmt.Errorf("supervisor: list active plans: %w", err)
	}
	total := 0
	for i, p := range plans {
		if i >= maxEnqueueDispatchPlans {
			break
		}
		n, err := s.orch.DispatchNext(ctx, p.ProjectID, p.ID)
		if err != nil {
			s.log("dispatch failed", "plan", p.ID, "err", err)
			continue
		}
		total += n
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

// HandleStatus reports supervisor-level liveness. ActiveWorkers is sourced
// from the store's real worker rows (store.CountRunningWorkers) rather
// than an in-process map: no in-process structure could ever reflect this
// count anyway, since agent subprocess lifetime is fully owned by whichever
// provider runner orch dispatched, not by the supervisor itself. A query
// failure degrades to 0 rather than failing the whole status reply — a
// transient count-query error should never make `status` itself fail.
func (s *Supervisor) HandleStatus(ctx context.Context) (ipc.StatusReply, error) {
	active, err := s.store.CountRunningWorkers(ctx)
	if err != nil {
		s.log("count running workers failed", "err", err)
		active = 0
	}

	return ipc.StatusReply{
		PID:           os.Getpid(),
		Uptime:        time.Since(s.startedAt),
		ActiveWorkers: active,
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

// HandleAttach streams no events yet — the durable event/attach surface is
// part of the plan-orchestration work in a later phase. It blocks until
// ctx is cancelled so a connected client simply sees a quiet, still-open
// stream rather than an immediate close.
func (s *Supervisor) HandleAttach(ctx context.Context, _ func(json.RawMessage) error) error {
	<-ctx.Done()
	return nil
}
