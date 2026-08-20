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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/jbcom/radioactive-ralph/internal/wsldistro"
)

// RalphWSLDistroName is the dedicated, auto-provisioned WSL2 distro provider
// turns dispatch into. Defaults to wsldistro.DistroName (the same package
// that owns auto-provisioning, see startPTY below) rather than duplicating
// the literal -- a package var, not a const, only so tests in this package
// can point it at an already-registered stand-in distro (e.g.
// "docker-desktop") instead of the not-yet-provisioned default. When
// overridden away from wsldistro.DistroName, auto-provisioning is skipped
// entirely: an override means the caller is deliberately using an
// already-registered distro, not asking Ralph to manage one.
var RalphWSLDistroName = wsldistro.DistroName

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

// Stat forwards to the output pipe, matching *os.File's behavior on Unix
// (ptyMaster.Stat exists because Unix tests call a.ptmx.Stat() to observe
// the pty master's post-close state; there is no equivalent Windows test
// today, but the interface method must still be satisfied).
func (w *wslDispatchPTY) Stat() (os.FileInfo, error) { return w.out.Stat() }

func startPTY(cmd *exec.Cmd, oneShotInput bool) (ptyMaster, error) {
	wslPath, err := exec.LookPath("wsl.exe")
	if err != nil {
		return nil, ErrWSLNotFound
	}

	// Auto-provisioning happens HERE, lazily, on first real dispatch
	// attempt -- not eagerly at --init, which is deliberately kept
	// lightweight and side-effect-minimal (see init_cmd.go's own doc
	// comment on what --init does and doesn't touch). A fast no-op once
	// registered (isRegistered is a single RegisteredDistros call); the
	// one-time ~68MB import only happens the first time a turn actually
	// needs to dispatch. Deliberately does not fall back to
	// ErrPTYUnsupported-style silence on failure: a missing bundled rootfs
	// or a failed import is a real, actionable error the operator needs to
	// see, not something to paper over.
	//
	// Skipped entirely when RalphWSLDistroName has been overridden away
	// from wsldistro.DistroName -- see that var's doc comment. Tests point
	// it at an already-registered stand-in distro precisely so they don't
	// need a real rootfs bundled next to the test binary.
	if RalphWSLDistroName == wsldistro.DistroName {
		if err := wsldistro.EnsureRegistered(context.Background()); err != nil {
			return nil, fmt.Errorf("agent: provision %q WSL2 distro: %w", wsldistro.DistroName, err)
		}
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
