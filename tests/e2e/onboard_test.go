package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestE2E_FirstRunWizardDeclinePath drives the guided first-run wizard under a
// real pty (so both stdin and stdout ARE terminals and the wizard actually
// runs). It exercises the safe decline-everything branch — no service is
// installed — proving the interactive path renders its consent prompt, reads
// keystrokes, and falls back to the manual commands. The happy path (actually
// installing a service) is deliberately NOT exercised here: registering a
// launchd/systemd unit is a real system mutation unsuitable for CI.
func TestE2E_FirstRunWizardDeclinePath(t *testing.T) {
	bin := BuildBinary(t)
	env := NewIsolatedEnv(t)
	env.MaterializeFixture(t)

	// Spawn the plain client cold — no supervisor running. Under the pty,
	// onboardingInteractive() is true, so the wizard runs.
	p := StartPTY(t, bin, env)
	defer p.Close()

	// The consent prompt: names what would be created, then asks to install.
	p.Expect("Ralph can set this up", 10*time.Second)
	p.Expect("state dir:", 5*time.Second)
	p.Expect("Install the background service", 5*time.Second)

	// Decline the service install.
	p.Send([]byte("n\n"))

	// It then offers the foreground fallback; decline that too.
	p.Expect("Run a foreground supervisor", 5*time.Second)
	p.Send([]byte("n\n"))

	// Falls back to the manual commands and exits non-zero.
	p.Expect("no supervisor is running", 5*time.Second)

	err := p.Wait(10 * time.Second)
	if err == nil {
		t.Fatal("client exited 0 after declining the wizard; want a non-zero exit (no supervisor)")
	}

	// Sanity: no service unit should have been written into the isolated HOME.
	out := string(p.snapshot())
	if strings.Contains(out, "installed supervisor service definition") {
		t.Errorf("a service was installed on the decline path; output:\n%s", out)
	}
}
