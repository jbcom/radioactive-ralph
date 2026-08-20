//go:build windows

package wsldistro

import (
	"context"
	"testing"
)

// TestCheckReportsWSLUnavailableWhenWslExeMissing is the deterministic case
// testable without a real WSL install or GoWSL's gowslmock build tag (which
// this package doesn't require as a test dependency): point PATH at an
// empty directory and confirm Check reports WSLAvailable: false rather than
// erroring or hanging.
func TestCheckReportsWSLUnavailableWhenWslExeMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	st := Check(context.Background())
	if !st.Applicable {
		t.Fatal("Check reported not applicable on windows")
	}
	if st.WSLAvailable {
		t.Fatal("Check reported WSL available with no wsl.exe on PATH")
	}
	if st.DistroRegistered {
		t.Fatal("Check reported a registered distro despite WSL being unavailable")
	}
}
