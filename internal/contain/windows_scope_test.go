package contain

import (
	"runtime"
	"testing"
)

// TestWindowsNeedsNoContainmentBecauseProvidersCannotRunThere records WHY the
// platform matrix has no Windows row, so the gap reads as a decision rather
// than an omission someone should close.
//
// Native Windows never runs a provider: agent.Start allocates a pty through
// creack/pty, which returns ErrPTYUnsupported there, and the Windows SCM
// safety spec states it outright — "Native Windows provider workers are
// already unsupported". Windows operators run Ralph under WSL, which is Linux,
// so Linux containment is what actually covers them.
//
// Building a Windows containment primitive would therefore guard a code path
// that cannot execute. This repo treats dead code as a defect, and an
// unverified containment claim is worse than an honest refusal.
//
// The assertion is deliberately about the CONTRACT rather than the platform:
// if native Windows ever gains a provider path, Available() there stops being
// trivially correct, and this test is the reminder to revisit the matrix.
func TestWindowsNeedsNoContainmentBecauseProvidersCannotRunThere(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("statement is about the windows build")
	}
	if Available() {
		t.Fatal("containment reports available on native Windows, but no provider " +
			"can run there (agent.Start returns ErrPTYUnsupported). Either a " +
			"provider path was added — in which case this platform now needs a " +
			"real, behaviorally-proven primitive — or Available() is wrong.")
	}
}
