//go:build windows

package agent

// Native Windows cannot allocate a real pty: creack/pty has no Windows
// implementation, and ConPTY -- the OS-shipped kernel32.dll implementation
// and the officially "fixed" redistributable conpty.dll alike -- cannot
// deliver a clean stdin EOF to a hosted child. Both were tested directly and
// reproduced the identical failure: a write into the pty's input channel is
// never observed by the child, and closing the write end never propagates
// as EOF, across three independent child runtimes. This is a confirmed,
// still-open upstream limitation (see
// https://github.com/microsoft/terminal/discussions/15006), not something
// fixable in this package.
//
// Instead, this dispatches the command through `wsl.exe` as an ordinary
// Windows subprocess -- no pty, no ConPTY, no CGO. wsl.exe forwards real
// Unix pipe/EOF semantics into the WSL2 guest, which was verified directly:
// a plain os/exec-spawned wsl.exe round-trips write -> close -> child
// observes EOF -> clean exit correctly, on the first try. Full evidence
// trail and the managed-distro architecture:
// docs/superpowers/specs/2026-08-20-windows-wsl2-dispatch-design.md.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// RalphWSLDistroName is the dedicated, auto-provisioned WSL2 distro provider
// turns dispatch into. Not yet wired to real user-facing configuration (see
// the design spec's open questions) -- a package var, not a const, only so
// tests in this package can point it at an already-registered distro
// instead of the not-yet-provisioned default.
var RalphWSLDistroName = "radioactive-ralph"

// ErrWSLNotFound is returned when wsl.exe is not on PATH. Distinct from
// ErrPTYUnsupported: unlike the pty story, this is fixable by installing
// WSL2, not a platform limitation.
var ErrWSLNotFound = fmt.Errorf("agent: wsl.exe not found on PATH; install WSL2 (see `radioactive_ralph doctor`)")

// wslDispatchPTY wraps the two plain pipes a wsl.exe subprocess's stdio
// flows through -- there is no single bidirectional fd the way a Unix pty
// master is, so this is a pairing, not a real ptyMaster in the Unix sense.
type wslDispatchPTY struct {
	out *os.File // read end: wsl.exe's merged stdout+stderr
	in  io.WriteCloser // write end: nil in the one-shot-input case (see startPTY)
}

func (w *wslDispatchPTY) Read(b []byte) (int, error) { return w.out.Read(b) }

func (w *wslDispatchPTY) Write(b []byte) (int, error) {
	if w.in == nil {
		return 0, ErrOneShotInputClosed
	}
	return w.in.Write(b)
}

func (w *wslDispatchPTY) Close() error {
	outErr := w.out.Close()
	if w.in == nil {
		return outErr
	}
	return errors.Join(outErr, w.in.Close())
}

// Fd exists only to satisfy ptyMaster's shape; disablePTYEcho is a no-op on
// Windows (there is no pty line discipline here to configure), so this
// value is never actually used for anything.
func (w *wslDispatchPTY) Fd() uintptr { return w.out.Fd() }

func startPTY(cmd *exec.Cmd, oneShotInput bool) (ptyMaster, error) {
	wslPath, err := exec.LookPath("wsl.exe")
	if err != nil {
		return nil, ErrWSLNotFound
	}

	// cmd.Args[0] is the ORIGINAL, unresolved command name (e.g. "cat"), not
	// cmd.Path -- exec.Command already ran that name through Windows-side
	// LookPath to build cmd.Path (e.g. Git-for-Windows' own cat.exe), which
	// is meaningless inside the Linux distro. Passing the resolved Windows
	// path across was a real bug caught by actually running this: wsl.exe
	// would try (and fail) to exec a Windows path as if it were a Linux one.
	// The unresolved name lets the distro's own shell/PATH resolve it.
	innerArgv := append([]string{}, cmd.Args...)
	cmd.Path = wslPath
	cmd.Args = append([]string{wslPath, "-d", RalphWSLDistroName, "--"}, innerArgv...)

	// cmd.Stdin may already be a bytes.Reader (the one-shot input case,
	// set by agent.Start before startPTY runs): os/exec's own copier
	// goroutine for a non-*os.File Stdin closes the underlying pipe at
	// EOF on its own, which is exactly the mechanism verified to deliver a
	// clean EOF through wsl.exe. Only create our own stdin pipe -- for
	// WriteInput's interactive path -- when there's no one-shot input.
	var stdin io.WriteCloser
	if !oneShotInput {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("agent: create wsl dispatch stdin pipe: %w", err)
		}
	}

	outR, outW, err := os.Pipe()
	if err != nil {
		if stdin != nil {
			_ = stdin.Close()
		}
		return nil, fmt.Errorf("agent: create wsl dispatch output pipe: %w", err)
	}
	// Merge stdout+stderr into one stream, matching a Unix pty master's
	// single combined channel (Agent.Output() is one stream, not two).
	cmd.Stdout = outW
	cmd.Stderr = outW

	if err := cmd.Start(); err != nil {
		_ = outR.Close()
		_ = outW.Close()
		if stdin != nil {
			_ = stdin.Close()
		}
		return nil, err
	}
	// Our copy of the write end; the child (wsl.exe) holds its own.
	_ = outW.Close()

	return &wslDispatchPTY{out: outR, in: stdin}, nil
}
