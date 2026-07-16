package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/agent"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
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

	// Logger receives lifecycle messages. Optional.
	Logger func(msg string, args ...any)
}

// Supervisor is the small, boring control-plane process described in spec
// §4/§13: pty ownership + IPC + store + reaper. It deliberately does NOT
// do plan orchestration yet (that lands in Phase 6) — HandleStatus and
// HandleStop are the only commands it answers today.
type Supervisor struct {
	opts      Options
	store     *store.Store
	server    *ipc.Server
	listener  *Listener
	sessionID string

	mu     sync.Mutex
	agents map[string]*agent.Agent

	startedAt time.Time
	log       func(msg string, args ...any)

	stopCh   chan struct{}
	stopOnce *sync.Once
}

// Run acquires the supervisor socket (failing with ErrSupervisorRunning if
// another instance already holds it), registers a supervisor session in
// the store, serves IPC until ctx is cancelled or a client sends CmdStop,
// and then shuts down cleanly: agents killed, IPC server stopped, socket
// released, session closed.
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

	sup := &Supervisor{
		opts:      opts,
		store:     opts.Store,
		listener:  listener,
		agents:    make(map[string]*agent.Agent),
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

// tick runs one reaper pass: reclaim stale worker/session claims and
// refresh this supervisor's own session heartbeat so peers/reapers never
// mistake it for stale.
func (s *Supervisor) tick(ctx context.Context) {
	if _, err := s.store.ReclaimStale(ctx, staleAfter); err != nil {
		s.log("reaper tick failed", "err", err)
	}
	if err := s.store.HeartbeatSession(ctx, s.sessionID); err != nil {
		s.log("session heartbeat failed", "err", err)
	}
}

// shutdown kills every tracked agent, stops the IPC server (which also
// unlinks the socket file), closes the supervisor's session row, and
// releases the PID lock. Errors are collected and joined so one failure
// doesn't skip the rest of teardown.
func (s *Supervisor) shutdown(ctx context.Context) error {
	s.mu.Lock()
	agents := make([]*agent.Agent, 0, len(s.agents))
	for id, a := range s.agents {
		agents = append(agents, a)
		delete(s.agents, id)
	}
	s.mu.Unlock()

	var errs []error
	for _, a := range agents {
		if err := a.Kill(); err != nil {
			errs = append(errs, fmt.Errorf("kill agent: %w", err))
		}
	}
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

// HandleStatus reports supervisor-level liveness. Plan/task/worker counts
// are intentionally zeroed for now — plan orchestration is Phase 6, so
// there is nothing yet to count; wiring those fields to real queries is
// noted as follow-up in the phase report, not silently faked here.
func (s *Supervisor) HandleStatus(_ context.Context) (ipc.StatusReply, error) {
	s.mu.Lock()
	active := len(s.agents)
	s.mu.Unlock()

	return ipc.StatusReply{
		PID:           os.Getpid(),
		Uptime:        time.Since(s.startedAt),
		ActiveWorkers: active,
	}, nil
}

// HandleEnqueue is not yet implemented — plan/task orchestration is Phase
// 6. Returning an explicit error here (rather than a silent no-op) means a
// client that tries this today gets a clear "not supported yet" rather
// than a false-positive success.
func (s *Supervisor) HandleEnqueue(_ context.Context, _ ipc.EnqueueArgs) (ipc.EnqueueReply, error) {
	return ipc.EnqueueReply{}, fmt.Errorf("supervisor: enqueue not implemented yet (plan orchestration lands in a later phase)")
}

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
