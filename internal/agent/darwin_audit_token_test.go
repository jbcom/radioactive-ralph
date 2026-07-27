//go:build darwin

package agent

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
)

func TestDarwinAuditTokenRejectsChangedPIDVersion(t *testing.T) {
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	api, err := systemDarwinProcessAPI()
	if err != nil {
		t.Fatalf("systemDarwinProcessAPI: %v", err)
	}
	token, err := api.auditTokenForPID(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("auditTokenForPID: %v", err)
	}
	if err := api.signalAuditToken(token, syscall.SIGCONT); err != nil {
		t.Fatalf("valid audit token probe: %v", err)
	}

	token[7]++ // pidversion: exact process-execution identity.
	if err := api.signalAuditTokenRaw(token, syscall.SIGCONT); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("changed pidversion probe = %v, want ESRCH", err)
	}
}

// TestDarwinAuditTokenReportsGoneForExitedProcess pins the contract that PTY
// session cleanup depends on. task_name_for_pid(2) reports a dead PID with a
// Mach code and never sets ESRCH, so a member that exits between enumeration
// and lookup must surface as errDarwinProcessGone. Without this, cleanup
// returns a hard error for an ordinary teardown race and reports the false
// cleanup failure issue #205 exists to eliminate.
func TestDarwinAuditTokenReportsGoneForExitedProcess(t *testing.T) {
	cmd := exec.Command("sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	pid := cmd.Process.Pid

	api, err := systemDarwinProcessAPI()
	if err != nil {
		t.Fatalf("systemDarwinProcessAPI: %v", err)
	}
	// Prove the PID resolves while the process is alive, so a "gone" result
	// after exit cannot be blamed on an unrelated lookup failure.
	if _, err := api.auditTokenForPID(pid); err != nil {
		t.Fatalf("auditTokenForPID while live: %v", err)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	// Reap it so the PID names no live process rather than a zombie.
	_ = cmd.Wait()

	_, err = api.auditTokenForPID(pid)
	if err == nil {
		t.Fatal("auditTokenForPID after exit = nil, want errDarwinProcessGone")
	}
	if !errors.Is(err, errDarwinProcessGone) {
		t.Fatalf("auditTokenForPID after exit = %v, want errDarwinProcessGone", err)
	}
	// The sentinel must not be mistakable for ESRCH: the previous code matched
	// ESRCH here and therefore never triggered.
	if errors.Is(err, syscall.ESRCH) {
		t.Fatal("vanished-process error reports ESRCH; Darwin does not set it here")
	}
}

func TestSortDarwinMembersLeafFirst(t *testing.T) {
	members := []darwinSessionMember{
		{pid: 10, parentPID: 1},
		{pid: 30, parentPID: 20},
		{pid: 20, parentPID: 10},
		{pid: 40, parentPID: 10},
	}
	sortDarwinMembersLeafFirst(members)
	got := []int{members[0].pid, members[1].pid, members[2].pid, members[3].pid}
	want := []int{30, 40, 20, 10}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want leaf-first %v", got, want)
		}
	}
}
