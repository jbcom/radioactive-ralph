// Package agent runs a single AI-agent CLI subprocess under Ralph's own
// pty, so Ralph owns its stdio and can stream, control, and kill it. The
// developer never touches this terminal — Ralph does, as the control layer.
package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"

	"github.com/creack/pty"
)

// ErrPTYUnsupported is returned by Start on platforms where creack/pty
// cannot allocate a pseudo-terminal — in practice, native Windows, where
// pty.Start returns pty.ErrUnsupported because there is no ConPTY-backed
// implementation. Ralph's control model requires owning the agent's pty, so
// on Windows operators run Ralph under WSL. Callers can match this with
// errors.Is to distinguish "this host can't host agents" from a transient
// spawn failure.
var ErrPTYUnsupported = fmt.Errorf("agent: pty allocation is unsupported on %s; run radioactive-ralph under WSL on Windows", runtime.GOOS)

// Options configures one agent subprocess.
type Options struct {
	Command    string
	Args       []string
	Dir        string
	Env        []string
	ResultPath string // file the CLI is told to write its structured result to
}

// Agent is a pty-owned agent subprocess.
type Agent struct {
	cmd  *exec.Cmd
	ptmx *os.File
	out  chan []byte
	done chan struct{}
	opts Options

	// closeOnce guards ptmx.Close so the readLoop's natural-exit close and
	// a caller's Kill can't double-close (which would return os.ErrClosed).
	closeOnce sync.Once
}

// Start launches opts.Command under a pty and begins streaming its output.
func Start(ctx context.Context, opts Options) (*Agent, error) {
	cmd := exec.CommandContext(ctx, opts.Command, opts.Args...) //nolint:gosec // G204: launching the configured agent CLI is this package's entire purpose; opts.Command/Args come from caller-controlled provider config, not untrusted external input
	cmd.Dir = opts.Dir
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	ptmx, err := pty.Start(cmd)
	if err != nil {
		if errors.Is(err, pty.ErrUnsupported) {
			return nil, ErrPTYUnsupported
		}
		return nil, err
	}
	a := &Agent{
		cmd:  cmd,
		ptmx: ptmx,
		out:  make(chan []byte, 256),
		done: make(chan struct{}),
		opts: opts,
	}
	go a.readLoop()
	return a, nil
}

func (a *Agent) readLoop() {
	defer close(a.out)
	defer close(a.done)
	scanner := bufio.NewScanner(a.ptmx)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		line = append(line, '\n')
		select {
		case a.out <- line:
		default:
			// Never block the reader on a slow consumer; drop to keep
			// the pty draining (the watchdog only needs recent signal).
		}
	}
	_ = a.cmd.Wait()
	a.closePTY()
}

// closePTY closes the pty exactly once, whether from the readLoop's
// natural exit or from Kill.
func (a *Agent) closePTY() {
	a.closeOnce.Do(func() { _ = a.ptmx.Close() })
}

// exited reports whether the process has already exited (readLoop finished).
func (a *Agent) exited() bool {
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

// Output is the line-oriented output stream; closed when the process exits.
func (a *Agent) Output() <-chan []byte { return a.out }

// WriteInput writes raw bytes to the agent's pty stdin. Per spec §1, agents
// run non-interactively and need little/no input; this exists for the
// providers that drive a CLI's stdin-based protocol (e.g. `claude -p
// --input-format stream-json`, which reads one JSON-line user message per
// turn) rather than passing the whole prompt as an argv/file. Direct
// Write() to the ptmx, per spec §2.
func (a *Agent) WriteInput(b []byte) error {
	_, err := a.ptmx.Write(b)
	return err
}

// Done is closed when the process exits.
func (a *Agent) Done() <-chan struct{} { return a.done }

// Kill terminates the process immediately and releases the pty. Killing an
// agent that already exited on its own is a no-op success — a normal
// shutdown that races an agent finishing its task must not surface a
// spurious "already closed" error.
func (a *Agent) Kill() error {
	if a.exited() {
		a.closePTY() // idempotent; the readLoop already closed it
		return nil
	}
	if a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
	}
	a.closePTY()
	return nil
}

// Wait blocks until the process exits.
func (a *Agent) Wait() error { <-a.done; return nil }

// PID returns the subprocess PID (0 before start / after release).
func (a *Agent) PID() int {
	if a.cmd.Process == nil {
		return 0
	}
	return a.cmd.Process.Pid
}
