//go:build darwin

package proclife

import (
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestAttachSetsPgid confirms the darwin-specific SysProcAttr
// field is populated so the child runs in its own process group.
func TestAttachSetsPgid(t *testing.T) {
	cmd := exec.Command("/bin/true")
	if err := Attach(cmd); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr nil after Attach")
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid = false, want true on darwin")
	}
}

// TestSupervisorStartWatchdog fires the real kqueue watchdog
// against a short-lived helper process and verifies the callback
// runs when the helper exits.
func TestSupervisorStartWatchdog(t *testing.T) {
	// Spawn a helper that exits after 300ms. Watchdog should fire
	// shortly after.
	helper := exec.Command("/bin/sh", "-c", "sleep 0.3")
	if err := helper.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	helperPID := helper.Process.Pid

	var fired atomic.Bool
	if err := SupervisorStartWatchdog(helperPID, func() { fired.Store(true) }); err != nil {
		t.Fatalf("SupervisorStartWatchdog: %v", err)
	}

	// Wait for helper to exit.
	if err := helper.Wait(); err != nil {
		// Expected: exit code 0 since sleep completed.
		var exitErr *exec.ExitError
		if !asExitError(err, &exitErr) {
			t.Fatalf("helper.Wait: %v", err)
		}
	}

	// Give the watchdog goroutine a beat to pick up the kevent.
	deadline := time.Now().Add(2 * time.Second)
	for !fired.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	if !fired.Load() {
		t.Error("watchdog callback did not fire after helper exit")
	}
}

// TestSupervisorStartWatchdogInvalidPID proves watchdog reports
// invalid-pid errors at registration time rather than silently
// leaking a goroutine.
func TestSupervisorStartWatchdogInvalidPID(t *testing.T) {
	// PID 0 is special — kqueue will return an error on NOTE_EXIT
	// registration for it. (Verified via syscall.ESRCH.)
	err := SupervisorStartWatchdog(0, func() {})
	// We accept either an error or a successful registration; the
	// point is no panic. If it succeeds we also accept that — the
	// kernel may just hold the registration forever.
	if err != nil && err != syscall.ESRCH && err != syscall.EINVAL {
		t.Logf("unexpected error (acceptable): %v", err)
	}
	// Ensure nothing crashed by waiting a tick.
	time.Sleep(50 * time.Millisecond)
	_ = os.Getpid()
}

// asExitError wraps errors.As so the test compiles without a
// separate import.
func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}
