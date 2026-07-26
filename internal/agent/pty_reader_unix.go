//go:build !windows

package agent

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const ptyReadPollMilliseconds = 10

// nonblockingPTY uses readiness polling for both sides of the PTY. O_NONBLOCK
// belongs to the shared open-file description, so making reads interruptible
// must be paired with full-write retry semantics rather than exposing short
// writes/EAGAIN to providers.
type nonblockingPTY struct {
	fd           int
	pollFD       int32
	readStop     <-chan struct{}
	terminal     <-chan struct{}
	onWriteBlock func()
}

func newInterruptiblePTY(
	file *os.File,
	readStop <-chan struct{},
	terminal <-chan struct{},
	onWriteBlock func(),
) (interruptiblePTY, error) {
	fd := int(file.Fd())
	if fd < 0 || fd > int(^uint32(0)>>1) {
		return nil, errors.New("agent: pty file descriptor is outside poll range")
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		return nil, err
	}
	return &nonblockingPTY{
		fd:           fd,
		pollFD:       int32(fd), //nolint:gosec // range checked immediately above
		readStop:     readStop,
		terminal:     terminal,
		onWriteBlock: onWriteBlock,
	}, nil
}

func (p *nonblockingPTY) Read(buffer []byte) (int, error) {
	for {
		select {
		case <-p.readStop:
			return 0, errOutputStopped
		default:
		}

		pollTimeout := ptyReadPollMilliseconds
		select {
		case <-p.terminal:
			// Once process control is terminal, drain only bytes already ready
			// in the kernel. A regrouped or setsid descendant may retain the
			// slave indefinitely; it must not keep Agent.Wait blocked.
			pollTimeout = 0
		default:
		}
		poll := []unix.PollFd{{Fd: p.pollFD, Events: unix.POLLIN}}
		ready, err := unix.Poll(poll, pollTimeout)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if ready == 0 {
			if pollTimeout == 0 {
				return 0, io.EOF
			}
			continue
		}

		n, err := syscall.Read(p.fd, buffer)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			continue
		}
		if n < 0 {
			// syscall.Read uses -1 on several Unix error paths; io.Reader's
			// contract requires a non-negative byte count alongside the error.
			n = 0
		}
		if n == 0 && err == nil {
			return 0, io.EOF
		}
		return n, err
	}
}

func (p *nonblockingPTY) WriteAll(ctx context.Context, buffer []byte) error {
	for len(buffer) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.terminal:
			return os.ErrClosed
		default:
		}

		n, err := syscall.Write(p.fd, buffer)
		if n > 0 {
			buffer = buffer[n:]
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) ||
			(n == 0 && err == nil) {
			if p.onWriteBlock != nil {
				p.onWriteBlock()
			}
			poll := []unix.PollFd{{Fd: p.pollFD, Events: unix.POLLOUT}}
			_, pollErr := unix.Poll(poll, ptyReadPollMilliseconds)
			if errors.Is(pollErr, syscall.EINTR) {
				continue
			}
			if pollErr != nil {
				return pollErr
			}
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}
