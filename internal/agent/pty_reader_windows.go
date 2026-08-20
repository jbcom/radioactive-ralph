//go:build windows

package agent

import (
	"context"
	"io"
)

// windowsPTY adapts a ptyMaster (wslDispatchPTY on Windows -- see
// pty_start_windows.go) to interruptiblePTY's WriteAll contract. Unlike the
// Unix nonblockingPTY, there is no readiness-polling/EAGAIN retry loop
// needed here: these are plain blocking pipes to a real subprocess, not a
// pty line discipline.
type windowsPTY struct {
	master ptyMaster
}

func newInterruptiblePTY(
	master ptyMaster,
	_ <-chan struct{},
	_ <-chan struct{},
	_ func(),
) (interruptiblePTY, error) {
	return &windowsPTY{master: master}, nil
}

func (p *windowsPTY) Read(buffer []byte) (int, error) {
	return p.master.Read(buffer)
}

func (p *windowsPTY) WriteAll(ctx context.Context, buffer []byte) error {
	for len(buffer) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := p.master.Write(buffer)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
		buffer = buffer[n:]
	}
	return nil
}
