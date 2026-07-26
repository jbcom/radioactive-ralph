// Package agent runs a single AI-agent CLI subprocess under Ralph's own
// pty, so Ralph owns its stdio and can stream, control, and kill it. The
// developer never touches this terminal — Ralph does, as the control layer.
package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/creack/pty"
)

// ErrPTYUnsupported is returned by Start on platforms where creack/pty cannot
// allocate a pseudo-terminal. Native Windows operators run Ralph under WSL.
var ErrPTYUnsupported = fmt.Errorf("agent: pty allocation is unsupported on %s; run radioactive-ralph under WSL on Windows", runtime.GOOS)

// ErrProcessExitObservationUnsupported reports a host on which Ralph cannot
// observe child exit without reaping it.
var ErrProcessExitObservationUnsupported = fmt.Errorf(
	"agent: non-reaping process-exit observation is unsupported on %s",
	runtime.GOOS,
)

// ErrProcessExitObservation is a static boundary for a failed kernel exit
// observer. Ralph explicitly terminates and reaps the child before returning
// this failure whenever direct-child termination succeeds.
var ErrProcessExitObservation = errors.New("agent: process-exit observation failed")

// ErrProcessSessionCleanup means the direct child reached a terminal outcome
// but Ralph could not prove that every member of the PTY's original process
// session was reclaimed. A descendant that creates another session with
// setsid(2) is outside this boundary and cannot be discovered portably.
var ErrProcessSessionCleanup = errors.New("agent: process-session cleanup failed")

// ErrProcessTreeCleanup is the compatibility name retained for callers of the
// v5 process-group contract. It matches ErrProcessSessionCleanup with
// errors.Is.
var ErrProcessTreeCleanup = ErrProcessSessionCleanup

// ErrProcessTermination means neither group termination nor the stable direct
// process handle could prove termination. The Agent releases its own control
// goroutines and returns this explicit failure instead of blocking forever or
// claiming that the child was reaped.
var ErrProcessTermination = errors.New("agent: direct-child termination failed")

// Options configures one agent subprocess.
type Options struct {
	Command    string
	Args       []string
	Dir        string
	Env        []string
	ResultPath string // file the CLI is told to write its structured result to

	// MaxOutputRetentionBytes bounds the aggregate provider-output bytes retained
	// inside the reader/watch pipeline. Zero selects
	// DefaultMaxOutputRetentionBytes. The line-retention threshold is derived
	// from this single budget; it is not a provider protocol-size assertion.
	MaxOutputRetentionBytes int

	// MaxObservedOutputBytes bounds cumulative raw bytes read from the pty,
	// including bytes in partial or discarded oversized lines. Zero disables
	// this work/liveness ceiling.
	MaxObservedOutputBytes int64

	// OversizeOutputPolicy chooses whether a line larger than the derived
	// retention threshold terminates the agent or is drained and discarded.
	OversizeOutputPolicy OversizeOutputPolicy

	// DisableEcho turns OFF the pty's terminal echo before the child starts.
	DisableEcho bool

	// Package-private lifecycle boundaries let deterministic tests inject
	// permanent kernel-observer and termination failures before any goroutine
	// starts. Production callers cannot set these fields.
	waitExitedForTest    func(func() (bool, error), <-chan struct{}) error
	probeExitedForTest   func(*os.Process) (bool, error)
	terminateTreeForTest func(*os.Process) terminationOutcome
	cleanupTreeForTest   func(*os.Process) error
	onWriteBlockForTest  func()
}

type interruptiblePTY interface {
	io.Reader
	WriteAll(context.Context, []byte) error
}

// Agent is a pty-owned agent subprocess.
type Agent struct {
	ctx  context.Context
	cmd  *exec.Cmd
	ptmx *os.File
	pty  interruptiblePTY
	opts Options

	out       chan []byte
	discarded chan []byte
	activity  chan time.Time
	// terminal closes after either successful cmd.Wait ownership or an explicit
	// unrecoverable termination failure. It is independent of output delivery.
	terminal     chan struct{}
	terminalOnce sync.Once
	done         chan struct{}

	maxRetainedLineBytes int

	closeOnce sync.Once
	killOnce  sync.Once
	killed    chan struct{}

	abandonOnce   sync.Once
	abandonOutput chan struct{}

	lifecycle  processLifecycle
	goroutines sync.WaitGroup

	controlMu  sync.Mutex
	controlErr error
	outputMu   sync.Mutex
	outputErr  error
	writeMu    sync.Mutex

	// Injectable OS boundaries support deterministic lifecycle failure tests.
	// Production initializes all four to the platform implementations.
	waitExited    func(func() (bool, error), <-chan struct{}) error
	probeExited   func(*os.Process) (bool, error)
	terminateTree func(*os.Process) terminationOutcome
	cleanupTree   func(*os.Process) error
}

// Start launches opts.Command under a pty and begins streaming its output.
func Start(ctx context.Context, opts Options) (*Agent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !processExitObservationSupported {
		return nil, ErrProcessExitObservationUnsupported
	}
	lineRetentionBytes, err := normalizeOutputRetention(&opts)
	if err != nil {
		return nil, err
	}
	if opts.MaxObservedOutputBytes < 0 {
		return nil, ErrInvalidObservedOutputLimit
	}

	// Ralph, not exec.CommandContext, owns termination and reaping.
	cmd := exec.Command(opts.Command, opts.Args...) //nolint:gosec // configured provider CLI is intentionally launched
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
		if err := disablePTYEcho(ptmx); err != nil {
			return nil, abortStartedProcess(
				cmd,
				ptmx,
				fmt.Errorf("agent: disable pty echo: %w", err),
				terminateProcessTree,
			)
		}
	}
	killed := make(chan struct{})
	terminal := make(chan struct{})
	ptyIO, err := newInterruptiblePTY(ptmx, killed, terminal, opts.onWriteBlockForTest)
	if err != nil {
		return nil, abortStartedProcess(
			cmd,
			ptmx,
			fmt.Errorf("agent: configure interruptible pty reader: %w", err),
			terminateProcessTree,
		)
	}

	a := &Agent{
		ctx:                  ctx,
		cmd:                  cmd,
		ptmx:                 ptmx,
		pty:                  ptyIO,
		opts:                 opts,
		out:                  make(chan []byte),
		discarded:            make(chan []byte),
		activity:             make(chan time.Time, 1),
		terminal:             terminal,
		done:                 make(chan struct{}),
		maxRetainedLineBytes: lineRetentionBytes,
		killed:               killed,
		abandonOutput:        make(chan struct{}),
		waitExited:           waitProcessExited,
		probeExited:          processAlreadyExited,
		terminateTree:        terminateProcessTree,
		cleanupTree:          cleanupExitedProcessTree,
	}
	if opts.waitExitedForTest != nil {
		a.waitExited = opts.waitExitedForTest
	}
	if opts.probeExitedForTest != nil {
		a.probeExited = opts.probeExitedForTest
	}
	if opts.terminateTreeForTest != nil {
		a.terminateTree = opts.terminateTreeForTest
	}
	if opts.cleanupTreeForTest != nil {
		a.cleanupTree = opts.cleanupTreeForTest
	}
	a.goroutines.Add(3)
	go func() {
		defer a.goroutines.Done()
		a.readLoop()
	}()
	go func() {
		defer a.goroutines.Done()
		a.observeProcessExit()
	}()
	go func() {
		defer a.goroutines.Done()
		a.cancelOnContext()
	}()
	return a, nil
}

// abortStartedProcess handles failures after pty.Start but before Agent
// goroutines exist. A successful termination is synchronously reaped. An
// impossible direct-handle kill returns its explicit error without calling
// cmd.Wait on a possibly-live child.
func abortStartedProcess(
	cmd *exec.Cmd,
	ptmx *os.File,
	primary error,
	terminate func(*os.Process) terminationOutcome,
) error {
	outcome := terminate(cmd.Process)
	_ = ptmx.Close()
	if outcome.terminationErr != nil {
		return errors.Join(primary, outcome.cleanupErr, outcome.terminationErr)
	}
	return errors.Join(primary, outcome.cleanupErr, cmd.Wait())
}

func (a *Agent) cancelOnContext() {
	select {
	case <-a.ctx.Done():
		_ = a.terminateProcess()
	case <-a.terminal:
	}
}

func (a *Agent) observeProcessExit() {
	err := a.waitExited(
		func() (bool, error) {
			return a.lifecycle.observe(func() (bool, error) {
				return a.probeExited(a.cmd.Process)
			})
		},
		a.terminal,
	)
	if errors.Is(err, errProcessExitObservationStopped) {
		return
	}
	if err != nil {
		a.addControlError(errors.Join(ErrProcessExitObservation, err))
		// Observer failure is terminal for the turn, but not for control:
		// explicit termination owns its own cmd.Wait convergence.
		_ = a.terminateProcess()
		return
	}
	_ = a.reapNatural()
}

func (a *Agent) reapNatural() error {
	claimed, cleanupErr := a.lifecycle.claimNatural(func() error {
		return a.cleanupTree(a.cmd.Process)
	})
	if !claimed {
		<-a.terminal
		return a.controlError()
	}
	if cleanupErr != nil {
		a.addControlError(errors.Join(ErrProcessTreeCleanup, cleanupErr))
	}
	a.lifecycle.finish(a.cmd.Wait())
	a.publishTerminal()
	return a.controlError()
}

// terminateProcess takes explicit forced ownership. A successful signal moves
// the lifecycle to reaping while holding the same mutex, then this goroutine
// calls cmd.Wait directly; observer health and Output consumers are irrelevant.
func (a *Agent) terminateProcess() error {
	claim := a.lifecycle.claimTermination(
		func() (bool, error) { return a.probeExited(a.cmd.Process) },
		func() terminationOutcome { return a.terminateTree(a.cmd.Process) },
	)
	if claim.observationErr != nil {
		a.addControlError(errors.Join(ErrProcessExitObservation, claim.observationErr))
	}
	if claim.cleanupErr != nil {
		a.addControlError(claim.cleanupErr)
	}
	if claim.natural {
		return a.reapNatural()
	}
	if claim.terminationErr != nil {
		a.addControlError(claim.terminationErr)
		a.releaseOutput()
		a.publishTerminal()
		return a.controlError()
	}
	if !claim.claimed {
		<-a.terminal
		return a.controlError()
	}

	a.releaseOutput()
	a.lifecycle.finish(a.cmd.Wait())
	a.publishTerminal()
	return a.controlError()
}

func (a *Agent) releaseOutput() {
	a.killOnce.Do(func() { close(a.killed) })
	a.closePTY()
}

func (a *Agent) publishTerminal() {
	a.terminalOnce.Do(func() { close(a.terminal) })
}

func (a *Agent) addControlError(err error) {
	if err == nil {
		return
	}
	a.controlMu.Lock()
	a.controlErr = errors.Join(a.controlErr, err)
	a.controlMu.Unlock()
}

func (a *Agent) controlError() error {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.controlErr
}

func (a *Agent) closePTY() {
	a.closeOnce.Do(func() { _ = a.ptmx.Close() })
}

// Output is the unbuffered line-oriented output stream. PTY EOF ends output
// collection but does not close this channel until process control reaches a
// natural or explicit terminal result.
func (a *Agent) Output() <-chan []byte { return a.out }

// WriteInput delivers the complete byte slice to the PTY or returns an error.
// The PTY is nonblocking so lifecycle failure can interrupt reads; writes
// therefore retry short/EAGAIN results behind readiness polling and honor
// caller cancellation or a terminal process result.
func (a *Agent) WriteInput(b []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return a.pty.WriteAll(a.ctx, b)
}

// Done closes after Output and all reader-side resources are released. A
// direct-child termination failure also closes Done, but Wait returns
// ErrProcessTermination and does not claim the process was reclaimed.
func (a *Agent) Done() <-chan struct{} { return a.done }

// Kill explicitly terminates and reaps the child when termination succeeds.
// Prefer TerminateAndWait at provider boundaries so reader/observer goroutines
// are also joined and cleanup errors cannot be ignored.
func (a *Agent) Kill() error { return a.terminateProcess() }

// TerminateAndWait is the provider cleanup boundary. It takes explicit
// termination ownership, abandons unread output, and joins every Agent-owned
// goroutine. It never reports success unless reclamation was proven.
func (a *Agent) TerminateAndWait() error {
	_ = a.terminateProcess()
	return a.Wait()
}

// Wait abandons unread Output and joins Agent-owned goroutines after a natural
// or explicit terminal-control result. It is passive: PTY EOF alone never
// authorizes termination. Observer, tree-cleanup, termination, and output
// failures are joined in its return value.
func (a *Agent) Wait() error {
	a.abandonOnce.Do(func() { close(a.abandonOutput) })
	<-a.done
	a.goroutines.Wait()
	return errors.Join(a.controlError(), a.OutputErr())
}

// ExitErr returns only a naturally exited child's cmd.Wait status. Forced or
// unrecoverable termination paths return nil because their cause is reported
// by Kill, TerminateAndWait, or Wait.
func (a *Agent) ExitErr() error { return a.lifecycle.exitError() }

// PID returns the direct child's PID only while its lifecycle is running.
func (a *Agent) PID() int { return a.lifecycle.pid(a.cmd.Process) }
