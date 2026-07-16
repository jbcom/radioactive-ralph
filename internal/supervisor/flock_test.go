//go:build !windows

package supervisor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAcquirePIDLockWritesOwnPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "supervisor.pid")
	f, err := acquirePIDLock(path)
	if err != nil {
		t.Fatalf("acquirePIDLock: %v", err)
	}
	defer func() { _ = f.Close() }()

	raw, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	got, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse pid file content %q: %v", raw, err)
	}
	if got != os.Getpid() {
		t.Errorf("pid file contains %d, want own pid %d", got, os.Getpid())
	}
}

// TestAcquirePIDLockSecondAttemptFails confirms the flock is genuinely
// exclusive: a second acquirePIDLock on the same path, while the first
// lock is still held, must fail — this is the mutex of last resort Acquire
// depends on to distinguish a live supervisor from a stale socket.
func TestAcquirePIDLockSecondAttemptFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "supervisor.pid")
	first, err := acquirePIDLock(path)
	if err != nil {
		t.Fatalf("first acquirePIDLock: %v", err)
	}
	defer func() { _ = first.Close() }()

	if _, err := acquirePIDLock(path); err == nil {
		t.Error("second acquirePIDLock while first is held: want error, got nil")
	}
}

// TestAcquirePIDLockReplacesStaleContent confirms a fresh acquire
// truncates and overwrites any leftover content from a previous
// (now-released) lock rather than appending or leaving stale bytes behind.
func TestAcquirePIDLockReplacesStaleContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "supervisor.pid")
	if err := os.WriteFile(path, []byte("99999999\nstale-trailing-garbage"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	f, err := acquirePIDLock(path)
	if err != nil {
		t.Fatalf("acquirePIDLock: %v", err)
	}
	defer func() { _ = f.Close() }()

	raw, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := strings.TrimSpace(string(raw))
	if content != strconv.Itoa(os.Getpid()) {
		t.Errorf("pid file content = %q, want exactly %d (stale content must be replaced, not appended)", content, os.Getpid())
	}
}

func TestShouldReclaimMissingPIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.pid")
	reclaim, err := shouldReclaim(path)
	if err != nil {
		t.Fatalf("shouldReclaim: %v", err)
	}
	if !reclaim {
		t.Error("shouldReclaim(missing pid file) = false, want true (nothing to protect the reclaim from)")
	}
}

func TestShouldReclaimDeadPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dead.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(1<<30)+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	reclaim, err := shouldReclaim(path)
	if err != nil {
		t.Fatalf("shouldReclaim: %v", err)
	}
	if !reclaim {
		t.Error("shouldReclaim(dead pid) = false, want true")
	}
}

func TestShouldReclaimLivePID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	reclaim, err := shouldReclaim(path)
	if err != nil {
		t.Fatalf("shouldReclaim: %v", err)
	}
	if reclaim {
		t.Error("shouldReclaim(own live pid) = true, want false (a live process holds this pid)")
	}
}

func TestShouldReclaimMalformedPIDFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "malformed.pid")
	if err := os.WriteFile(path, []byte("not-a-number\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := shouldReclaim(path); err == nil {
		t.Error("shouldReclaim(malformed pid file): want error, got nil")
	}
}
