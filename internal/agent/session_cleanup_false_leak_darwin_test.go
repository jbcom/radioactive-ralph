//go:build darwin

package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestCleanupDoesNotReportAReapedProcessAsLive is the regression test for a
// FALSE leak report.
//
// `remaining` is built from enumeration ALONE: a member in the already-
// signalled process group is appended and then `continue`d past every liveness
// check, so the only way a pid leaves the list is disappearing from a LATER
// kern.proc.all enumeration. That enumeration lags a completed reap under
// load -- there is a window after the kernel kills a process, before it
// becomes a zombie, in which it still enumerates as live, and
// darwinSessionMembers only filters darwinZombieState.
//
// The symptom was a turn that had ALREADY SUCCEEDED failing with
// "still has live members after cleanup", wrapping ErrProcessSessionCleanup
// around a correct result and breaking errors.Is for callers.
//
// This drives the real syscalls rather than faking them: currentSession and
// readDarwinSessionMember are package-level functions with no injection seam,
// so a fake would prove nothing about the code that actually runs.
func TestCleanupDoesNotReportAReapedProcessAsLive(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "tree.sh")
	// sh -> sleep, the exact shape the provider fakes use: the child inherits
	// the leader's process group, which is precisely the member that takes the
	// unchecked `continue` path.
	// `sleep 300 & wait`, not a bare `sleep 300`: as the shell's LAST command
	// the latter can be exec-optimised into the shell process itself,
	// collapsing the two-process tree this test needs.
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 300 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid
	sessionID := currentSession(pid)

	// Let the shell fork its sleep child into the session.
	deadline := time.Now().Add(2 * time.Second)
	sawTree := false
	for time.Now().Before(deadline) {
		if members, err := darwinSessionMembers(sessionID); err == nil && len(members) > 1 {
			sawTree = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Fail rather than proceed: without the child, this exercises a
	// single-process session and silently stops testing what it claims to.
	if !sawTree {
		t.Fatalf("never observed a multi-member session for pid %d; the "+
			"sh->sleep tree this test depends on was never established", pid)
	}

	// Kill the whole group, exactly as the cleanup path's caller does, so
	// every member is already reaped or reaping by the time cleanup runs.
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_, _ = cmd.Process.Wait()

	// HONEST SCOPE: this test does NOT exercise the re-verification --
	// TestDeadlineDropsAStaleEnumeratedPidButKeepsALiveOne below does, via the
	// reVerifyStillLive seam, and it FAILS when the fix is removed. Measured on an
	// idle host, kern.proc.all drops every member IMMEDIATELY after the group
	// SIGKILL -- 2 members before, 0 at +0ms, +2ms and +20ms -- so `remaining`
	// is empty, cleanup returns at the len(remaining)==0 check, and the
	// deadline branch never runs. Mutation confirms it: deleting the re-check
	// entirely, and forcing the budget to zero, both leave this GREEN.
	//
	// The enumeration lag the bug depends on cannot be produced on demand
	// without faking the syscall, and a fake would prove nothing about the
	// code that actually runs. So this test guards the END-TO-END contract --
	// killing a real sh->sleep tree must not report a leak -- and
	// TestReVerificationDropsAPidThatIsNoLongerInTheSession below pins the
	// predicate the fix is built from, deterministically.
	restore := cleanupBudget
	cleanupBudget = 0
	defer func() { cleanupBudget = restore }()

	// Cleanup must not report a leak for processes the kernel has already
	// killed.
	if err := cleanupOriginalProcessSession(cmd.Process, true); err != nil {
		t.Fatalf("cleanup reported a leak for an already-killed session: %v", err)
	}
}

// TestReVerificationDropsAPidThatIsNoLongerInTheSession pins the re-check
// itself, deterministically, without depending on reproducing the enumeration
// lag that triggers the bug in production.
//
// The behavioural test above cannot prove the fix: on an idle machine the reap
// lands within one 5ms pass, so `remaining` empties on its own and the test
// passes with OR without the re-verification. (Confirmed by mutation -- it
// still passed with the fix removed.) A test that passes either way proves
// nothing, so this one asserts the predicate the fix is built from.
//
// A reaped pid is exactly what a lagging enumeration eventually converges to.
//
// WHAT THIS DOES AND DOES NOT PROVE, stated because three mutations survived
// it and I would rather record that than let the test look stronger than it
// is. A fully reaped pid makes SysctlKinfoProc return EIO, so `err != nil`
// short-circuits the predicate before `found` or the session comparison is
// ever consulted. Mutating either of those halves -- making an errored read
// report found=true, or making currentSession return the caller's group on
// error -- leaves this test GREEN.
//
// So this pins the ONE branch that actually fires for a reaped pid: an
// errored read must not be treated as a live member. That is the branch the
// production bug needs, and it is worth pinning. It is not a proof of the
// whole predicate, and the deadline branch itself stays unreachable without
// faking the syscall.
func TestReVerificationDropsAPidThatIsNoLongerInTheSession(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	dead := cmd.Process.Pid

	// This is the predicate the deadline branch applies to every entry of
	// `remaining`. For a pid the kernel has finished with, it must be false --
	// otherwise cleanup reports a leak for a process that is gone.
	// NOTE, found by running this: a fully reaped pid makes SysctlKinfoProc
	// return EIO -- not ESRCH or ENOENT, the two errors
	// readDarwinSessionMember translates to found=false. So the predicate
	// rejects it via `err == nil` rather than via `found`. Asserting the
	// mechanism I assumed (found=false) would have asserted the wrong thing.
	_, found, err := readDarwinSessionMember(dead)
	if err == nil && found && currentSession(dead) == currentSession(os.Getpid()) {
		t.Fatalf("a reaped pid (%d) still satisfies the liveness predicate "+
			"(err=%v found=%v); cleanup would report it as a live leaked member",
			dead, err, found)
	}
}

// TestDeadlineDropsAStaleEnumeratedPidButKeepsALiveOne is the test the other
// two could not be: it FAILS when the deadline re-check is removed.
//
// The seam is reVerifyStillLive. The behavioural test above cannot reach the
// deadline branch at all -- on an idle host kern.proc.all drops every member
// within one poll -- so this injects the lagging enumeration directly, which
// is the only way to exercise the branch without a real saturated machine.
func TestDeadlineDropsAStaleEnumeratedPidButKeepsALiveOne(t *testing.T) {
	// A pid the kernel is finished with: exactly what a lagging enumeration
	// keeps reporting after the reap.
	done := exec.Command("/bin/sh", "-c", "exit 0")
	if err := done.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	stale := done.Process.Pid

	t.Run("stale pid is dropped, no leak reported", func(t *testing.T) {
		got := reVerifyStillLive([]int{stale}, currentSession(os.Getpid()))
		if len(got) != 0 {
			t.Fatalf("re-verify kept %v for a reaped pid; cleanup would report "+
				"a leak for a process the kernel has already finished with", got)
		}
	})

	// FAIL CLOSED: a pid that is genuinely alive in this session must survive
	// the re-check, or a real leaked worker would vanish from the report.
	t.Run("live in-session pid is kept", func(t *testing.T) {
		self := os.Getpid()
		got := reVerifyStillLive([]int{self}, currentSession(self))
		if len(got) != 1 || got[0] != self {
			t.Fatalf("re-verify dropped a LIVE in-session pid (%d), got %v; a "+
				"real leak would be silently reported as clean", self, got)
		}
	})
}
