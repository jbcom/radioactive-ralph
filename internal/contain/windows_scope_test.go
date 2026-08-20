package contain

import (
	"runtime"
	"testing"
)

// TestWindowsNeedsNoContainmentBecauseTheNativeProcessNeverWrites records WHY
// the platform matrix has no Windows row, so the gap reads as a decision
// rather than an omission someone should close.
//
// UPDATED per docs/superpowers/specs/2026-08-20-windows-wsl2-dispatch-design.md:
// this test's previous docstring ("native Windows never runs a provider,
// agent.Start allocates a pty through creack/pty, which returns
// ErrPTYUnsupported there") is no longer true and was rewritten rather than
// left stale — that was ConPTY-era framing before ConPTY was confirmed a
// dead end (it cannot deliver a clean stdin EOF to a hosted child, a real,
// still-open upstream limitation) and replaced with dispatch through
// wsl.exe as a plain subprocess.
//
// The reason this platform still needs no containment primitive is
// different now, not absent: the native Windows process (agent.Start on
// windows) is just wsl.exe's *launcher* — it writes nothing itself, and the
// actual provider CLI runs as a genuine Linux process inside the managed
// WSL2 distro. Containment for that process is a Linux containment
// question, covered by this package's existing Linux path, IF it is ever
// wired into the distro at all — that's still open, not decided here. This
// test only asserts the native Windows side needs no primitive of its own,
// which remains true under the new architecture for a different reason than
// the old one.
//
// The assertion is deliberately about the CONTRACT rather than the
// platform: if the native Windows process itself ever gains a real write
// path (not just launching wsl.exe), Available() there stops being
// trivially correct, and this test is the reminder to revisit the matrix.
func TestWindowsNeedsNoContainmentBecauseTheNativeProcessNeverWrites(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("statement is about the windows build")
	}
	if Available() {
		t.Fatal("containment reports available on native Windows, but the native " +
			"process is only wsl.exe's launcher and writes nothing itself. Either " +
			"the native process gained a real write path — in which case this " +
			"platform now needs a real, behaviorally-proven primitive — or " +
			"Available() is wrong.")
	}
}
