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
