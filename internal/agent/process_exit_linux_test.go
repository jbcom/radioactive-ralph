//go:build linux

package agent

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRetryInterruptedSyscall(t *testing.T) {
	attempts := 0
	err := retryInterruptedSyscall(func() error {
		attempts++
		if attempts < 3 {
			return unix.EINTR
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("retry result = (%v, attempts=%d), want (nil, 3)", err, attempts)
	}

	sentinel := errors.New("permanent")
	attempts = 0
	err = retryInterruptedSyscall(func() error {
		attempts++
		return sentinel
	})
	if !errors.Is(err, sentinel) || attempts != 1 {
		t.Fatalf("permanent result = (%v, attempts=%d), want (sentinel, 1)", err, attempts)
	}
}
