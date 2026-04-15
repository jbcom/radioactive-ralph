// Package supervisor is the per-variant brain of radioactive-ralph.
//
// One Supervisor instance runs per live `ralph run --variant X`
// invocation. It owns:
//
//   - A PID flock at Paths.Sessions/<variant>.pid so only one
//     Supervisor-per-variant runs at a time in the same repo.
//   - The Unix socket listener at Paths.Sessions/<variant>.sock and
//     its heartbeat marker .alive.
//   - The SQLite event log at Paths.StateDB. On boot, the event log is
//     replayed to rebuild in-memory state.
//   - The workspace.Manager that owns mirror/worktree lifecycle.
//   - The session pool of claude subprocesses.
//
// Lifecycle:
//
//	New()         → resolve state paths, validate variant
//	Run(ctx)      → blocks; does flock acquire, db open, socket bind,
//	                event replay, workspace init, session pool spawn,
//	                IPC request loop; returns nil on graceful shutdown
//	                or a descriptive error on startup failure.
//	Shutdown(ctx) → asks the running supervisor to stop gracefully.
package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/db"
	"github.com/jbcom/radioactive-ralph/internal/ipc"
	"github.com/jbcom/radioactive-ralph/internal/variant"
	"github.com/jbcom/radioactive-ralph/internal/workspace"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
)

// Options configure a Supervisor.
type Options struct {
	// RepoPath is the operator's repo (mandatory).
	RepoPath string

	// Variant profile to drive. Mandatory.
	Variant variant.Profile

	// Workspace is the workspace manager for this variant run. Caller
	// constructs it (we can't — it needs resolved knobs from config).
	Workspace *workspace.Manager

	// HeartbeatInterval controls how often the supervisor touches the
	// .alive file. Default 10s.
	HeartbeatInterval time.Duration

	// ShutdownTimeout is the max wall time to wait for graceful
	// shutdown before force-killing sessions. Default 30s.
	ShutdownTimeout time.Duration
}

// Supervisor coordinates a single variant's run.
type Supervisor struct {
	opts   Options
	paths  xdg.Paths
	db     *db.DB
	server *ipc.Server

	// pidFile is the flock-held file path; empty until Run acquires it.
	pidFile string
	pidFD   *os.File // released on Shutdown

	// started is when Run() began, used for Uptime in status.
	started time.Time

	// shutdownOnce guards Shutdown from running twice.
	shutdownOnce sync.Once
	shutdownCh   chan struct{}
}

// New constructs a Supervisor without starting it. Returns an error if
// the options are structurally invalid; all runtime failures surface
// from Run.
func New(opts Options) (*Supervisor, error) {
	if opts.RepoPath == "" {
		return nil, errors.New("supervisor: RepoPath required")
	}
	if opts.Workspace == nil {
		return nil, errors.New("supervisor: Workspace required")
	}
	if opts.Variant.Name == "" {
		return nil, errors.New("supervisor: Variant required")
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 10 * time.Second
	}
	if opts.ShutdownTimeout <= 0 {
		opts.ShutdownTimeout = 30 * time.Second
	}

	paths, err := xdg.Resolve(opts.RepoPath)
	if err != nil {
		return nil, fmt.Errorf("supervisor: resolve paths: %w", err)
	}

	return &Supervisor{
		opts:       opts,
		paths:      paths,
		shutdownCh: make(chan struct{}),
	}, nil
}

// Run blocks until the supervisor shuts down. Returns nil for a
// graceful stop, error for any startup or fatal runtime failure.
func (s *Supervisor) Run(ctx context.Context) error {
	s.started = time.Now()

	if err := s.paths.Ensure(); err != nil {
		return fmt.Errorf("ensure state dirs: %w", err)
	}

	// Flock first — if another supervisor owns this variant, fail fast.
	pidPath := filepath.Join(s.paths.Sessions,
		fmt.Sprintf("%s.pid", s.opts.Variant.Name))
	fd, err := acquirePIDLock(pidPath)
	if err != nil {
		return fmt.Errorf("acquire pid lock %s: %w", pidPath, err)
	}
	s.pidFD = fd
	s.pidFile = pidPath

	// Open the event log.
	database, err := db.Open(ctx, s.paths.StateDB)
	if err != nil {
		s.cleanupPID()
		return fmt.Errorf("open state db: %w", err)
	}
	s.db = database

	// Replay event log — currently a no-op counter for M2; the
	// individual Reducer functions are filled in M3 as each event kind
	// grows semantics.
	replayed := 0
	if err := database.Replay(ctx, 0, func(db.Event) error {
		replayed++
		return nil
	}); err != nil {
		s.cleanupDB()
		s.cleanupPID()
		return fmt.Errorf("replay event log: %w", err)
	}

	// Workspace init (Idempotent — safe on re-runs).
	if err := s.opts.Workspace.Init(ctx); err != nil {
		s.cleanupDB()
		s.cleanupPID()
		return fmt.Errorf("workspace init: %w", err)
	}

	// Reconcile stale worktrees from a prior crashed run.
	if err := s.opts.Workspace.Reconcile(ctx); err != nil {
		s.cleanupDB()
		s.cleanupPID()
		return fmt.Errorf("workspace reconcile: %w", err)
	}

	// Bind the socket and spawn the IPC server.
	socketPath := filepath.Join(s.paths.Sessions,
		fmt.Sprintf("%s.sock", s.opts.Variant.Name))
	heartbeatPath := socketPath + ".alive"
	srv, err := ipc.NewServer(ipc.ServerOptions{
		SocketPath:    socketPath,
		HeartbeatPath: heartbeatPath,
		Handler:       &handler{sup: s},
	})
	if err != nil {
		s.cleanupDB()
		s.cleanupPID()
		return fmt.Errorf("ipc server: %w", err)
	}
	s.server = srv

	if err := srv.Start(s.opts.HeartbeatInterval); err != nil {
		s.cleanupDB()
		s.cleanupPID()
		return fmt.Errorf("ipc start: %w", err)
	}

	// Record our own startup in the event log so a stop-and-resume
	// shows the right arc of events.
	if err := s.logEvent(ctx, "supervisor.start", map[string]any{
		"variant":  string(s.opts.Variant.Name),
		"pid":      os.Getpid(),
		"replayed": replayed,
	}); err != nil {
		// Non-fatal — log it to stderr in a real deployment, but we
		// don't have a logger wired yet. Proceed.
		_ = err
	}

	// Wait for context cancellation or an explicit Shutdown call.
	select {
	case <-ctx.Done():
	case <-s.shutdownCh:
	}

	return s.gracefulShutdown(ctx)
}

// Shutdown requests a graceful stop. Safe to call from a signal
// handler or another goroutine. Run() returns after internal cleanup
// completes.
func (s *Supervisor) Shutdown() {
	s.shutdownOnce.Do(func() { close(s.shutdownCh) })
}

// gracefulShutdown tears down resources in the reverse of Run's
// acquisition order.
func (s *Supervisor) gracefulShutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(
		context.Background(), s.opts.ShutdownTimeout)
	defer cancel()

	// Prefer the caller's ctx deadline if it's stricter.
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < s.opts.ShutdownTimeout {
		shutdownCtx, cancel = context.WithDeadline(context.Background(), dl)
		defer cancel()
	}
	_ = shutdownCtx // for symmetry — per-session shutdown will use this in M3

	_ = s.logEvent(ctx, "supervisor.stop", map[string]any{
		"variant": string(s.opts.Variant.Name),
		"uptime":  time.Since(s.started).String(),
	})

	if s.server != nil {
		_ = s.server.Stop()
	}
	s.cleanupDB()
	s.cleanupPID()
	return nil
}

// logEvent appends an Event to the event log with a JSON payload.
func (s *Supervisor) logEvent(ctx context.Context, kind string, payload any) error {
	if s.db == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	_, err = s.db.Append(ctx, db.Event{
		Kind:       kind,
		Stream:     "supervisor",
		Actor:      string(s.opts.Variant.Name),
		PayloadRaw: raw,
	})
	return err
}

// cleanupPID closes and removes the PID lock file.
func (s *Supervisor) cleanupPID() {
	if s.pidFD != nil {
		_ = s.pidFD.Close()
		s.pidFD = nil
	}
	if s.pidFile != "" {
		_ = os.Remove(s.pidFile)
		s.pidFile = ""
	}
}

// cleanupDB closes the event log.
func (s *Supervisor) cleanupDB() {
	if s.db != nil {
		_ = s.db.Close()
		s.db = nil
	}
}
