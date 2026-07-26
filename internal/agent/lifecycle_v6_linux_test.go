//go:build linux

package agent

import (
	"fmt"
	"os"
	"testing"
)

func verifyNoLiveOriginalSessionDescendants(process *os.Process) error {
	if process == nil || process.Pid <= 1 {
		return nil
	}
	members, err := linuxSessionMembers(process.Pid)
	if err != nil {
		return err
	}
	live := make([]int, 0)
	for _, member := range members {
		if isLiveOriginalSessionMember(process.Pid, member) {
			live = append(live, member.pid)
		}
	}
	if len(live) == 0 {
		return nil
	}
	return fmt.Errorf(
		"original PTY session %d still has live descendants: %v",
		process.Pid,
		live,
	)
}

func isLiveOriginalSessionMember(
	sessionID int,
	member linuxSessionMember,
) bool {
	return member.session == sessionID &&
		member.pid != sessionID &&
		member.state != 'Z'
}

func TestLiveOriginalSessionMemberClassification(t *testing.T) {
	const sessionID = 42
	for _, tc := range []struct {
		name  string
		state byte
		want  bool
	}{
		{name: "running", state: 'R', want: true},
		{name: "sleeping", state: 'S', want: true},
		{name: "disk wait", state: 'D', want: true},
		{name: "stopped", state: 'T', want: true},
		{name: "zombie", state: 'Z', want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			member := linuxSessionMember{
				pid:     sessionID + 1,
				session: sessionID,
				group:   sessionID,
				state:   tc.state,
			}
			if got := isLiveOriginalSessionMember(sessionID, member); got != tc.want {
				t.Fatalf("state %q classified live=%v, want %v", tc.state, got, tc.want)
			}
		})
	}
	if isLiveOriginalSessionMember(sessionID, linuxSessionMember{
		pid:     sessionID,
		session: sessionID,
		group:   sessionID,
		state:   'R',
	}) {
		t.Fatal("direct session leader must be left to cmd.Wait")
	}
}
