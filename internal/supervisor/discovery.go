// Package supervisor implements the `--supervisor` process: the single
// durable authority described in docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md
// §4-§6. It owns the one user-level store, all agent ptys, and the IPC
// endpoint clients discover (§5c: "the socket is the advertisement").
// Single-instance is enforced by an exclusive flock on the PID file, not by
// the socket bind (which happens downstream of that lock).
package supervisor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/ipc"
)

// ErrNoSupervisor is returned by Find when no live supervisor answers a
// connect at runtimeDir's socket — either nothing is listening, the socket
// file is missing, or it is a stale leftover from a crashed process.
var ErrNoSupervisor = errors.New("supervisor: no supervisor is listening")

// ErrSupervisorRunning is returned by Acquire when another live supervisor
// already holds the lock. The actual single-instance mutex is the exclusive
// non-blocking flock on the PID file (acquirePIDLock); the socket
// advertisement (§5c) that clients discover is bound downstream of that lock
// (inside ipc.Server.Start). This error surfaces the second acquire's
// failure with a name callers can match via errors.Is.
var ErrSupervisorRunning = errors.New("supervisor: another supervisor is already running")

// dialTimeout bounds how long Find/Acquire wait for a connect to either
// succeed or definitively fail. Kept short: both callers are on an
// interactive CLI path and a live supervisor should answer almost
// instantly.
const dialTimeout = 2 * time.Second

// pidFileName is the lockfile Acquire uses to distinguish a live
// supervisor from a stale socket left by one that crashed. It lives
// alongside the socket/heartbeat files in runtimeDir.
const pidFileName = "supervisor.pid"

// endpointPaths resolves the socket + heartbeat + PID-lock paths for the
// supervisor listening under runtimeDir. runtimeDir is a plain directory
// (typically xdg StateRoot()) — reused via ipc.ServiceEndpoint because
// that already contains the correct per-OS socket/named-pipe logic; the
// "sessions dir" framing in that function's doc comment is a per-repo
// artifact of its original caller, not a constraint on this one.
func endpointPaths(runtimeDir string) (socketPath, heartbeatPath, pidPath string) {
	socketPath, heartbeatPath = ipc.ServiceEndpoint(runtimeDir)
	pidPath = filepath.Join(runtimeDir, pidFileName)
	return socketPath, heartbeatPath, pidPath
}

// Find tries to connect to the supervisor socket under runtimeDir. A
// successful connect means a live supervisor answered — the returned
// *ipc.Client is ready to use. Any failure (connect refused, socket
// missing, or a stale socket nothing is listening behind) collapses to
// ErrNoSupervisor: callers don't need to distinguish "never started" from
// "crashed," both mean the client should offer to start one (spec §4).
func Find(runtimeDir string) (*ipc.Client, error) {
	socketPath, _, _ := endpointPaths(runtimeDir)
	client, err := ipc.Dial(socketPath, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoSupervisor, err)
	}
	return client, nil
}

// Listener wraps the bound supervisor socket plus the resources that
// enforce single-instance: the *ipc.Server built on top of it and the PID
// lockfile held for this process's lifetime. Release must be called
// exactly once, typically via a deferred call from the owning Supervisor.
type Listener struct {
	SocketPath    string
	HeartbeatPath string
	pidLock       *os.File
	pidPath       string
}

// Release closes the PID lockfile (dropping the flock) and removes the PID
// file. It does NOT close the *ipc.Server bound to SocketPath — the caller
// owns that server's lifecycle separately (Server.Stop() also unlinks the
// socket file). Calling Release after the server has stopped is the
// expected order: server down, then mutex released.
func (l *Listener) Release() error {
	if l.pidLock == nil {
		return nil
	}
	err := l.pidLock.Close()
	_ = os.Remove(l.pidPath)
	l.pidLock = nil
	return err
}

// Acquire takes the single-instance lock for runtimeDir. The actual mutex
// is the exclusive non-blocking flock on the PID file (acquirePIDLock): a
// second live supervisor fails to take that lock and Acquire returns
// ErrSupervisorRunning. The socket clients discover (spec §5c: "the socket
// is the advertisement") is bound later, in ipc.Server.Start, strictly
// after Acquire has already won the PID lock — so two processes racing
// Acquire contend on the flock, never on the socket bind.
//
// Before taking the lock, Acquire checks whether the socket path is a
// stale leftover from a crashed supervisor: if a live client can still
// connect, a supervisor is genuinely running (ErrSupervisorRunning). If
// nothing answers, Acquire consults the PID lockfile — a dead recorded PID
// means the previous supervisor crashed without cleaning up, so Acquire
// reclaims: removes the stale socket file and takes over the PID lock
// itself. A missing or already-unlocked PID file is treated the same as a
// dead PID (nothing to protect the reclaim from).
func Acquire(runtimeDir string) (*Listener, error) {
	if runtimeDir == "" {
		return nil, fmt.Errorf("supervisor: runtimeDir required")
	}
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return nil, fmt.Errorf("supervisor: create runtime dir: %w", err)
	}

	socketPath, heartbeatPath, pidPath := endpointPaths(runtimeDir)

	if _, statErr := os.Stat(socketPath); statErr == nil {
		if client, err := ipc.Dial(socketPath, dialTimeout); err == nil {
			_ = client.Close()
			return nil, ErrSupervisorRunning
		}
		// Socket file exists but nothing answered: possibly stale. Confirm
		// via the PID lockfile before reclaiming, so a supervisor that is
		// merely slow to accept (not crashed) is not clobbered out from
		// under itself.
		if reclaim, err := shouldReclaim(pidPath); err != nil {
			return nil, err
		} else if !reclaim {
			return nil, ErrSupervisorRunning
		}
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("supervisor: remove stale socket: %w", err)
		}
		_ = os.Remove(heartbeatPath)
	}

	pidLock, err := acquirePIDLock(pidPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSupervisorRunning, err)
	}

	return &Listener{
		SocketPath:    socketPath,
		HeartbeatPath: heartbeatPath,
		pidLock:       pidLock,
		pidPath:       pidPath,
	}, nil
}

// shouldReclaim reports whether a stale-looking socket is safe to reclaim:
// true when the PID file is missing/empty (nothing recorded) or records a
// PID that is no longer alive on this host.
func shouldReclaim(pidPath string) (bool, error) {
	pid, err := readPIDFile(pidPath)
	if err != nil {
		return false, fmt.Errorf("supervisor: read pid lock: %w", err)
	}
	if pid == 0 {
		return true, nil
	}
	return !pidAlive(pid), nil
}
