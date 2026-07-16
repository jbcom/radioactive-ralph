// Package agent runs a single AI-agent CLI subprocess under Ralph's own
// pty, so Ralph owns its stdio and can stream, control, and kill it. The
// developer never touches this terminal — Ralph does, as the control layer.
package agent

import (
	"bufio"
	"context"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

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
	_ = a.ptmx.Close()
}

// Output is the line-oriented output stream; closed when the process exits.
func (a *Agent) Output() <-chan []byte { return a.out }

// Done is closed when the process exits.
func (a *Agent) Done() <-chan struct{} { return a.done }

// Kill terminates the process immediately and releases the pty.
func (a *Agent) Kill() error {
	if a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
	}
	return a.ptmx.Close()
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
