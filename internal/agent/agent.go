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

	// cmdCancel cancels the command's private context. Kill calls it to
	// terminate the process: exec.Cmd's Cancel (group-kill, via
	// setCancelKillsGroup) + WaitDelay coordinate the kill with exec's own
	// Process.Wait, so a signal can never land on an already-reaped/recycled
	// PID — unlike a direct killProcessTree(cmd.Process) from Kill, which would
	// race readLoop's cmd.Wait(). Idempotent (context.CancelFunc).
	cmdCancel context.CancelFunc

	// ctx is the caller's context, observed by readLoop's blocking send so a
	// consumer that stops reading after ctx cancellation (e.g. a runner that
	// returned on ctx.Done before the process fully exits) doesn't leak the
	// reader on a[.out send that no one will ever drain.
	ctx context.Context

	// exitErr records cmd.Wait()'s result — the process's exit status. It is
	// written once by readLoop (the sole Wait) and read via ExitErr() after
	// Done() closes. exitMu guards the cross-goroutine handoff. A nonzero exit
	// (auth failure, model error, mid-run crash) surfaces here so a runner
	// WITHOUT a terminal-frame gate (codex) can fail the turn instead of
	// laundering a failed CLI's partial output into a successful result.
	exitMu  sync.Mutex
	exitErr error
}

// Start launches opts.Command under a pty and begins streaming its output.
func Start(ctx context.Context, opts Options) (*Agent, error) {
	// Give the command a PRIVATE cancelable context (child of the caller's) so
	// Kill can terminate the process by cancelling it. Routing the kill through
	// exec.Cmd's own Cancel→Wait machinery — rather than calling
	// killProcessTree(cmd.Process) directly — is the only race-free option: exec
	// coordinates the group-kill with its internal Process.Wait, so a signal can
	// NEVER land after the PID was reaped (and possibly recycled by the kernel).
	// A direct kill from Kill() would race readLoop's own cmd.Wait() reaping.
	cmdCtx, cmdCancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cmdCtx, opts.Command, opts.Args...) //nolint:gosec // G204: launching the configured agent CLI is this package's entire purpose; opts.Command/Args come from caller-controlled provider config, not untrusted external input
	cmd.Dir = opts.Dir
	if opts.Env != nil {
		cmd.Env = opts.Env
	}
	// Route the automatic ctx-cancel kill through the whole process group, not
	// just the direct child (exec.CommandContext's default) — so a turn cancelled
	// by KillWorker / shutdown / stall-timeout / Kill reaps grandchildren too.
	// This also sets cmd.WaitDelay, bounding how long exec waits after Cancel
	// before force-killing so Wait returns promptly and a child ignoring the
	// group SIGKILL can't wedge the reaper.
	setCancelKillsGroup(cmd)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		cmdCancel() // nothing started; release the private context
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
			cmdCancel() // triggers exec's group-kill of the just-started child
			// Reap the killed child — readLoop (the only other Wait) hasn't
			// started yet on this early-return path, so without this Wait the
			// process would be left a zombie.
			_ = cmd.Wait()
			return nil, fmt.Errorf("agent: disable pty echo: %w", err)
		}
	}
	a := &Agent{
		ctx:       ctx,
		cmd:       cmd,
		cmdCancel: cmdCancel,
		ptmx:      ptmx,
		out:       make(chan []byte, 256),
		done:      make(chan struct{}),
		killed:    make(chan struct{}),
		opts:      opts,
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
	defer func() {
		// exec.Cmd.Wait() reaps the child. It coordinates with cmd.Cancel (from
		// Kill via cmdCancel) internally, so the group-kill never races this
		// reap — no PID-recycle hazard here.
		err := a.cmd.Wait()
		// Release the private command context on EVERY exit (natural or killed),
		// so it doesn't leak once the process is gone. Idempotent with Kill's
		// call.
		a.cmdCancel()
		a.exitMu.Lock()
		a.exitErr = err
		a.exitMu.Unlock()
	}()
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

	// Terminate the process by cancelling its private context. exec.Cmd's Cancel
	// (setCancelKillsGroup → group SIGKILL) + WaitDelay coordinate the kill with
	// exec's own Process.Wait, so the signal is guaranteed NOT to land on an
	// already-reaped (and possibly kernel-recycled) PID — the race a direct
	// killProcessTree(a.cmd.Process) here could not avoid, because readLoop's
	// cmd.Wait() may have reaped the child inside Process.Wait even though Wait
	// itself has not yet returned. cmdCancel is idempotent, so racing/repeated
	// Kills are safe. A no-op if the process already exited (Cancel on a
	// finished cmd does nothing).
	if a.cmdCancel != nil {
		a.cmdCancel()
	}

	// closePTY unblocks a scanner still parked on the ptmx so the reaper can
	// finish; idempotent with readLoop's own close.
	a.closePTY()
	return nil
}

// Wait blocks until the process exits.
func (a *Agent) Wait() error { <-a.done; return nil }

// ExitErr returns the subprocess's exit status once it has exited on its own
// (nil if it exited 0). It returns nil while the process is still running and
// nil when the process was KILLED by this Agent (a signal-death from Kill /
// ctx-cancel / a stall-or-prompt watchdog kill is not a real failure — the
// caller already knows it forced the exit). Call only after Output() closes /
// Done() fires. This lets a runner without a structured terminal frame (codex)
// distinguish a clean completion from a failed CLI exit, rather than treating
// any exit as success.
func (a *Agent) ExitErr() error {
	select {
	case <-a.killed:
		return nil // we forced the exit; the signal error is not a turn failure
	default:
	}
	a.exitMu.Lock()
	defer a.exitMu.Unlock()
	return a.exitErr
}

// PID returns the subprocess PID (0 before start / after release).
func (a *Agent) PID() int {
	if a.cmd.Process == nil {
		return 0
	}
	return a.cmd.Process.Pid
}
