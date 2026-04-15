//go:build linux

package proclife

import (
	"os/exec"
	"syscall"
	"testing"
)

// TestAttachSetsPdeathsig confirms the Linux-specific SysProcAttr
// field is populated so the kernel will SIGTERM the child when the
// parent exits.
func TestAttachSetsPdeathsig(t *testing.T) {
	cmd := exec.Command("/bin/true")
	if err := Attach(cmd); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr nil after Attach")
	}
	if cmd.SysProcAttr.Pdeathsig != syscall.SIGTERM {
		t.Errorf("Pdeathsig = %v, want SIGTERM", cmd.SysProcAttr.Pdeathsig)
	}
}
