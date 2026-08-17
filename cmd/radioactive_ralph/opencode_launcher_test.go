//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
)

func TestOpenCodeLauncherDispatchPreservesProviderExitBeforeCobra(t *testing.T) {
	t.Setenv(adapters.ManagedSessionEnv, "")
	t.Setenv(adapters.HookEndpointEnv, "")
	t.Setenv("PATH", t.TempDir())
	binary := writeLauncherCommandFixture(t, "#!/bin/sh\nexit 37\n")
	handled, code := maybeRunOpenCodeLauncher([]string{
		"radioactive_ralph", "hook", "launch-opencode", "--binary", binary, "--", "run",
	})
	if !handled || code != 37 {
		t.Fatalf("launcher dispatch = (%t, %d), want (true, 37)", handled, code)
	}
}

func TestOpenCodeLauncherDispatchPreservesProviderSignalBeforeCobra(t *testing.T) {
	t.Setenv(adapters.ManagedSessionEnv, "")
	t.Setenv(adapters.HookEndpointEnv, "")
	binary := writeLauncherCommandFixture(t, "#!/bin/sh\nkill -TERM $$\n")
	handled, code := maybeRunOpenCodeLauncher([]string{
		"radioactive_ralph", "hook", "launch-opencode", "--binary", binary,
	})
	if !handled || code != 143 {
		t.Fatalf("launcher dispatch = (%t, %d), want (true, 143)", handled, code)
	}
}

func TestOpenCodeLauncherDispatchRejectsPartialCoordinatesBeforeBundleLookup(t *testing.T) {
	t.Setenv(adapters.ManagedSessionEnv, "partial-session")
	t.Setenv(adapters.HookEndpointEnv, "")
	marker := filepath.Join(t.TempDir(), "launched")
	binary := writeLauncherCommandFixture(t, "#!/bin/sh\n: > \"$RALPH_TEST_MARKER\"\n")
	t.Setenv("RALPH_TEST_MARKER", marker)
	if err := exec.Command(binary).Run(); err != nil { //nolint:gosec // test-owned executable fixture
		t.Fatalf("control launch: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("control launch did not create marker: %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove control marker: %v", err)
	}
	handled, code := maybeRunOpenCodeLauncher([]string{
		"radioactive_ralph", "hook", "launch-opencode", "--binary", binary,
		"--adapter-root", filepath.Join(t.TempDir(), "missing"),
	})
	if !handled || code != 2 {
		t.Fatalf("partial launcher dispatch = (%t, %d), want (true, 2)", handled, code)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("partial launcher started provider: %v", err)
	}
}

func writeLauncherCommandFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatalf("write OpenCode fixture: %v", err)
	}
	return path
}
