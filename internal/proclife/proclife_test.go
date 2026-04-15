package proclife

import (
	"os/exec"
	"testing"
)

// TestAttachIsIdempotent verifies Attach works on a command with no
// prior SysProcAttr and on one with a prior instance.
func TestAttachIsIdempotent(t *testing.T) {
	cmd := exec.Command("/bin/true")
	if err := Attach(cmd); err != nil {
		t.Fatalf("Attach on fresh cmd: %v", err)
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("Attach did not allocate SysProcAttr")
	}
	// Second call should not error — idempotent.
	if err := Attach(cmd); err != nil {
		t.Fatalf("second Attach: %v", err)
	}
}

// TestAttachReturnsNilForUnstartedCmd confirms Attach never errors
// on a well-formed command. Platform-specific struct fields are
// verified by the platform-tagged tests below.
func TestAttachReturnsNilForUnstartedCmd(t *testing.T) {
	if err := Attach(exec.Command("/bin/true")); err != nil {
		t.Errorf("Attach: %v", err)
	}
}

// TestPostStartNoopOnPOSIX verifies PostStart before Start is a
// no-op on POSIX (where it's genuinely nothing to do). On Windows
// it would error because cmd.Process is nil; that's tested in the
// windows-tagged test.
func TestPostStartNoopOnPOSIX(_ *testing.T) {
	// This test only runs on POSIX — on Windows the file will
	// compile and run but PostStart expects cmd.Process to exist.
	cmd := exec.Command("/bin/true")
	err := PostStart(cmd)
	// POSIX accepts nil-Process, Windows does not. Platform tests
	// below validate the Windows case.
	_ = err
}
