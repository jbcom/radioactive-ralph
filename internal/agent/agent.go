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

	// DisableEcho turns OFF the pty's terminal echo before the child starts.
	// Providers that drive the CLI over stdin (claude/opencode stream-json)
	// MUST set this: otherwise the pty echoes every stdin line back onto the
	// output stream, where the never-block watchdog pattern-matches the
	// operator's OWN prompt text ("do you want to…", "approve", …) as an
	// interactive prompt and kills the turn. Disabling echo removes the
	// echoed line at the source. No effect on native Windows (no pty there).
	DisableEcho bool
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

	// killed is closed by Kill so the readLoop's output send can abandon a
	// blocked-or-dead consumer instead of either dropping lines silently
	// (which could drop a provider's terminal result frame and force a false
	// stall) or blocking the reader forever (violating the never-block
	// invariant). A live consumer drains a.out; a killed agent unblocks here.
	killOnce sync.Once
	killed   chan struct{}

	// ctx is the caller's context, observed by readLoop's blocking send so a
	// consumer that stops reading after ctx cancellation (e.g. a runner that
	// returned on ctx.Done before the process fully exits) doesn't leak the
	// reader on a[.out send that no one will ever drain.
	ctx context.Context
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
	if opts.DisableEcho {
		// Ordered correctly: providers write to stdin only AFTER Start
		// returns, and echo is a line-discipline property applied to those
		// later writes — so disabling it here removes the echo of every
		// stdin line the caller subsequently sends.
		if err := disablePTYEcho(ptmx); err != nil {
			_ = ptmx.Close()
			_ = killProcessTree(cmd.Process)
			// Reap the killed child — readLoop (the only other Wait) hasn't
			// started yet on this early-return path, so without this Wait the
			// process would be left a zombie.
			_ = cmd.Wait()
			return nil, fmt.Errorf("agent: disable pty echo: %w", err)
		}
	}
	a := &Agent{
		ctx:    ctx,
		cmd:    cmd,
		ptmx:   ptmx,
		out:    make(chan []byte, 256),
		done:   make(chan struct{}),
		killed: make(chan struct{}),
		opts:   opts,
	}
	go a.readLoop()
	return a, nil
}

func (a *Agent) readLoop() {
	// Reap the process and release the pty on EVERY exit path (natural EOF or
	// the killed-consumer abandon below), so neither leaks a zombie nor a fd.
	defer close(a.out)
	defer close(a.done)
	defer a.closePTY()
	defer func() { _ = a.cmd.Wait() }()
	scanner := bufio.NewScanner(a.ptmx)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		line = append(line, '\n')
		// Block until the consumer takes the line OR the agent is killed —
		// never silently drop (a dropped terminal result frame forces a
		// false stall) and never block forever (a killed agent unblocks via
		// a.killed, honoring the never-block invariant). A live consumer
		// keeps a.out draining, so this rarely blocks in practice.
		select {
		case a.out <- line:
		case <-a.killed:
			return
		case <-a.ctx.Done():
			// The caller cancelled: a consumer that returned on ctx.Done
			// will never drain a.out, so stop rather than block forever.
			return
		}
	}
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
	// Unblock a readLoop that may be parked on a full a.out (dead/slow
	// consumer) so it can exit instead of leaking the reader goroutine.
	a.killOnce.Do(func() { close(a.killed) })
	if a.exited() {
		a.closePTY() // idempotent; the readLoop already closed it
		return nil
	}
	if a.cmd.Process != nil {
		// Kill the whole process GROUP, not just the direct child: the agent CLI
		// may have spawned grandchildren (shell tools, git, an MCP server) that
		// would otherwise orphan against the checkout and keep running after this
		// returns. See killProcessTree.
		_ = killProcessTree(a.cmd.Process)
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
