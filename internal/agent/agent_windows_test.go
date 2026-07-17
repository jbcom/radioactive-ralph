//go:build windows

package agent

import (
	"context"
	"errors"
	"testing"
)

// TestStartUnsupportedOnWindows asserts the platform boundary: on native
// Windows, creack/pty cannot allocate a pty, so Start must fail fast with
// ErrPTYUnsupported (a clear "run under WSL" message) rather than a bare
// "unsupported". This is the Windows counterpart to the Unix pty tests in
// agent_test.go / watchdog_test.go, which are built only for !windows.
func TestStartUnsupportedOnWindows(t *testing.T) {
	a, err := Start(context.Background(), Options{Command: "cmd", Args: []string{"/c", "echo hi"}})
	if a != nil {
		_ = a.Kill()
		t.Fatalf("Start returned a live agent on Windows; expected ErrPTYUnsupported")
	}
	if !errors.Is(err, ErrPTYUnsupported) {
		t.Fatalf("Start error = %v, want ErrPTYUnsupported", err)
	}
}
