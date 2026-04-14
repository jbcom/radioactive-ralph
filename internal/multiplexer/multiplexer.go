// Package multiplexer detaches the Ralph supervisor from the calling
// shell so `ralph run --detach` returns control to the operator while
// the supervisor keeps running.
//
// Three backends are probed in order, from strongest to weakest:
//
//  1. tmux — if $TMUX is set or `tmux` is on PATH. Operator can later
//     re-attach with `tmux attach -t <session>` to see the live supervisor
//     output. This is the recommended backend on every platform that
//     has it.
//  2. screen — fallback if tmux is unavailable. Less featureful UI but
//     same attach/detach model.
//  3. setsid + double-fork — pure-stdlib fallback via syscalls. Runs the
//     supervisor as an orphaned process inherited by init (pid 1) with
//     stdin/stdout/stderr redirected to a log file. No re-attach is
//     possible in this mode — operators use `ralph attach` (Unix socket)
//     or tail the log directly.
//
// Service-installed variants (brew services, launchd, systemd --user)
// always invoke the supervisor in --foreground mode, bypassing this
// package entirely. The service manager itself is the supervisor's parent.
package multiplexer

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// Backend identifies which detach mechanism a Detacher will use.
type Backend int

const (
	// BackendUnknown is the zero value; only returned by Detect when
	// an explicit override asked for an unknown kind.
	BackendUnknown Backend = iota
	// BackendTmux uses `tmux new-session -d` for full detach with later re-attach.
	BackendTmux
	// BackendScreen uses `screen -dmS` for detach with later re-attach.
	BackendScreen
	// BackendSetsid uses syscall.Setsid + double-fork for pure-POSIX detach
	// with no re-attach UI (operator uses `ralph attach` or tails the log).
	BackendSetsid
)

// String returns a human-friendly backend name.
func (b Backend) String() string {
	switch b {
	case BackendTmux:
		return "tmux"
	case BackendScreen:
		return "screen"
	case BackendSetsid:
		return "setsid"
	case BackendUnknown:
		return "unknown"
	}
	return "unknown"
}

// Detacher encapsulates one chosen backend and knows how to spawn the
// supervisor command detached from the calling shell.
type Detacher struct {
	backend          Backend
	lookPath         func(string) (string, error) // swappable for tests
	getenv           func(string) string          // swappable for tests
	preferredBackend Backend                      // non-zero means caller pinned a choice
}

// ErrNoBackend is returned if Detect cannot find a usable backend. On
// current macOS/Linux this essentially never happens because the
// setsid fallback always succeeds.
var ErrNoBackend = errors.New("multiplexer: no backend available (no tmux, no screen, no setsid)")

// DetectOption customises Detect. These compose for tests where we
// need to force a specific backend or swap out the probe.
type DetectOption func(*Detacher)

// WithLookPath overrides exec.LookPath. Tests pass a stub that reports
// specific binaries as missing to exercise each fallback branch.
func WithLookPath(fn func(string) (string, error)) DetectOption {
	return func(d *Detacher) { d.lookPath = fn }
}

// WithGetenv overrides os.Getenv. Used by tests to toggle $TMUX.
func WithGetenv(fn func(string) string) DetectOption {
	return func(d *Detacher) { d.getenv = fn }
}

// WithPreferredBackend forces a specific backend if available; Detect
// will return ErrNoBackend if the preferred one probes as missing.
// Used by `ralph run --multiplexer X` CLI flag and by init wizard
// when the operator has explicitly chosen a backend.
func WithPreferredBackend(b Backend) DetectOption {
	return func(d *Detacher) { d.preferredBackend = b }
}

// Detect probes the environment and returns a Detacher bound to the
// strongest available backend.
func Detect(opts ...DetectOption) (*Detacher, error) {
	d := &Detacher{
		lookPath: exec.LookPath,
		getenv:   os.Getenv,
	}
	for _, o := range opts {
		o(d)
	}

	probe := func(b Backend) bool {
		switch b {
		case BackendTmux:
			if d.getenv("TMUX") != "" {
				return true
			}
			_, err := d.lookPath("tmux")
			return err == nil
		case BackendScreen:
			_, err := d.lookPath("screen")
			return err == nil
		case BackendSetsid:
			// setsid is a syscall (not an external binary) on every
			// POSIX system Ralph targets. Always available.
			return true
		case BackendUnknown:
			return false
		}
		return false
	}

	// If the caller pinned a specific backend, honor it or fail cleanly.
	if d.preferredBackend != BackendUnknown {
		if probe(d.preferredBackend) {
			d.backend = d.preferredBackend
			return d, nil
		}
		return nil, fmt.Errorf("multiplexer: preferred backend %q unavailable", d.preferredBackend)
	}

	// Default precedence.
	for _, b := range []Backend{BackendTmux, BackendScreen, BackendSetsid} {
		if probe(b) {
			d.backend = b
			return d, nil
		}
	}
	return nil, ErrNoBackend
}

// Backend reports which detach mechanism this Detacher is bound to.
func (d *Detacher) Backend() Backend { return d.backend }

// SpawnRequest describes a process the supervisor wants spawned detached.
// All file paths must be absolute; SpawnDetached does no path resolution.
type SpawnRequest struct {
	// Name is the command to exec. Usually the absolute path to ralph itself,
	// e.g. "/usr/local/bin/ralph".
	Name string

	// Args are passed to Name. Typically something like
	// ["_supervisor", "--variant", "green", "--repo-root", "..."].
	Args []string

	// SessionName is the human identifier (tmux session name, screen
	// session name, or a tag written into the log file header for setsid
	// mode). Should be unique per per-variant supervisor on this host;
	// the supervisor package generates it as
	// `ralph-<variant>-<repohash[:8]>`.
	SessionName string

	// LogPath receives stdout + stderr of the spawned supervisor (setsid
	// mode uses it as the primary record; tmux/screen sessions also
	// `pipe-pane` / `logfile` to it for `ralph attach` fallback tailing).
	LogPath string

	// Env is passed through to the detached process. Nil means inherit
	// the current environment.
	Env []string

	// Dir sets the working directory of the detached process. Supervisor
	// is typically spawned with Dir = operator's repo root.
	Dir string
}

// Spawned is the return value of SpawnDetached. Descriptor names how to
// re-reach the detached process (tmux/screen session name), and PID
// carries the child PID for backends that know it synchronously.
//
// SpawnDetached itself runs req.Name with req.Args fully detached. For
// tmux/screen it blocks only long enough to invoke the multiplexer's
// own detach command (~millisecond). For setsid it performs a
// double-fork and returns when the grandchild has execve'd.
type Spawned struct {
	// Descriptor names how to re-reach the detached process. For tmux:
	// the session name passed to `tmux attach -t <name>`. For screen:
	// the session name passed to `screen -r <name>`. For setsid: the
	// empty string (no re-attach available; use `ralph attach` socket).
	Descriptor string

	// PID is the process ID of the detached supervisor if the backend
	// knows it immediately. tmux/screen may not populate this
	// synchronously — call ralph status or read the PID file written by
	// the supervisor itself.
	PID int
}

// SpawnDetached is implemented per backend in platform-specific files.
// See multiplexer_unix.go for POSIX implementations and
// multiplexer_unsupported.go for the windows placeholder.
