//go:build darwin || linux

package provider

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadBoundedAuthoritativeResultFIFOReplacementCannotBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "result")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var raw []byte
	var readErr error
	go func() {
		defer close(done)
		raw, readErr = readBoundedAuthoritativeResultWithOpener(
			path,
			func(path string) (*os.File, error) {
				if err := os.Remove(path); err != nil {
					return nil, err
				}
				if err := syscall.Mkfifo(path, 0o600); err != nil {
					return nil, err
				}
				return openAuthoritativeResultFile(path)
			},
		)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("authoritative result read blocked on a FIFO substituted after Lstat")
	}
	if !errors.Is(readErr, ErrAuthoritativeResultUnsafe) {
		t.Fatalf("FIFO replacement error = %v, want ErrAuthoritativeResultUnsafe", readErr)
	}
	if raw != nil {
		t.Fatalf("FIFO replacement returned %q, want no bytes", raw)
	}
}
