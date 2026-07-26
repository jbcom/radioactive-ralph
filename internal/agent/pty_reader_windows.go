//go:build windows

package agent

import (
	"context"
	"io"
	"os"
)

// Native Windows pty execution is rejected by Start. Keep the compile-time
// boundary explicit for cross-build validation.
type windowsPTY struct{ file *os.File }

func newInterruptiblePTY(
	file *os.File,
	_ <-chan struct{},
	_ <-chan struct{},
	_ func(),
) (interruptiblePTY, error) {
	return &windowsPTY{file: file}, nil
}

func (p *windowsPTY) Read(buffer []byte) (int, error) {
	return p.file.Read(buffer)
}

func (p *windowsPTY) WriteAll(ctx context.Context, buffer []byte) error {
	for len(buffer) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := p.file.Write(buffer)
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
