//go:build !windows

package adapters

import "testing"

func TestOpenCodeLauncherPreservesProviderSignal(t *testing.T) {
	binary := writeOpenCodeLauncherFixture(t, "#!/bin/sh\nkill -TERM $$\n")
	if code := RunOpenCodeLauncher(OpenCodeLaunchOptions{Binary: binary}); code != 143 {
		t.Fatalf("signal exit = %d, want 143", code)
	}
}
