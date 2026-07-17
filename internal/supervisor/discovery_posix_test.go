//go:build !windows

package supervisor

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// TestAcquire_ReclaimsStaleSocketAfterCrash models POSIX socket-as-file
// semantics: a crashed supervisor leaves a real socket *file* plus a PID
// lock behind, and Acquire must reclaim by removing the stale socket and
// taking the lock. On Windows the endpoint is a named pipe (not a
// filesystem file you can pre-create with os.WriteFile), and the stale-pipe
// case is handled differently; the platform-independent reclaim decision
// (shouldReclaim on a plain PID file) is covered cross-platform in
// flock_test.go, so this file is Unix-only.
func TestAcquire_ReclaimsStaleSocketAfterCrash(t *testing.T) {
	runtimeDir := t.TempDir()

	// Simulate a supervisor that bound the socket and wrote its PID lock,
	// then crashed without running its own cleanup: the socket file and
	// PID file are left behind, but nothing is listening and the PID is
	// dead.
	socketPath, _, pidPath := endpointPaths(runtimeDir)
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	if err := os.WriteFile(socketPath, nil, 0o600); err != nil {
		t.Fatalf("write stale socket placeholder: %v", err)
	}
	deadPID := deadPIDForTest(t)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(deadPID)+"\n"), 0o600); err != nil {
		t.Fatalf("write stale pid file: %v", err)
	}

	l, err := Acquire(runtimeDir)
	if err != nil {
		t.Fatalf("Acquire() after crash err = %v, want nil (should reclaim)", err)
	}
	defer func() { _ = l.Release() }()
}

// deadPIDForTest returns a PID that is guaranteed not to be alive: it
// spawns a short-lived process, waits for it to exit, and returns its PID.
// Uses /bin/sh (POSIX) — this helper lives in the Unix-only test file.
func deadPIDForTest(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start throwaway process: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait throwaway process: %v", err)
	}
	// Give the OS a moment to fully reap so a liveness probe can't racily
	// observe a zombie as alive on platforms where that distinction leaks
	// through os.FindProcess/Signal(0).
	time.Sleep(50 * time.Millisecond)
	return pid
}

// TestAcquire_LiveButUnresponsiveSupervisorNotClobbered is the regression
// for the audit's coverage gap and the entire reason the PID flock exists:
// when the socket file is present but a dial fails (a supervisor that is
// merely slow to accept, not crashed) AND a live process still holds the PID
// lock, Acquire must return ErrSupervisorRunning WITHOUT deleting the socket
// — it must not clobber a supervisor out from under itself.
func TestAcquire_LiveButUnresponsiveSupervisorNotClobbered(t *testing.T) {
	runtimeDir := t.TempDir()

	// First Acquire wins the PID lock and holds it for the rest of the test,
	// standing in for a live supervisor process.
	live, err := Acquire(runtimeDir)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer func() { _ = live.Release() }()

	// Materialize a socket-file placeholder that nothing is listening behind,
	// so a dial will fail (mimicking a supervisor slow to accept). The live
	// PID lock is still held, so shouldReclaim must be false.
	socketPath, _, _ := endpointPaths(runtimeDir)
	if err := os.WriteFile(socketPath, nil, 0o600); err != nil {
		t.Fatalf("write unresponsive socket placeholder: %v", err)
	}

	// A second Acquire must refuse (live lock) and NOT remove the socket.
	if _, err := Acquire(runtimeDir); !errors.Is(err, ErrSupervisorRunning) {
		t.Fatalf("second Acquire err = %v, want ErrSupervisorRunning", err)
	}
	if _, statErr := os.Stat(socketPath); statErr != nil {
		t.Errorf("Acquire removed the socket of a live supervisor: %v", statErr)
	}
}
