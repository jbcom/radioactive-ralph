//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMaybeRunWindowsServiceRejectsLegacySCMBeforeConfigOrEnvironment(t *testing.T) {
	originalDetect := detectWindowsService
	detectWindowsService = func() (bool, error) { return true, nil }
	t.Cleanup(func() { detectWindowsService = originalDetect })

	configPath := filepath.Join(t.TempDir(), "legacy-config-must-not-exist.json")
	originalArgs := os.Args
	os.Args = []string{
		"radioactive_ralph.exe",
		"--supervisor",
		"--windows-service-config",
		configPath,
	}
	t.Cleanup(func() { os.Args = originalArgs })

	const key = "RALPH_WINDOWS_SERVICE_TEST_ENV"
	t.Setenv(key, "unchanged")

	handled, code := maybeRunWindowsService()
	if !handled || code != windowsSCMProcessGuardExitCode {
		t.Fatalf("maybeRunWindowsService = (%t, %d), want (true, %d)", handled, code, windowsSCMProcessGuardExitCode)
	}
	if code == 1 {
		t.Fatal("SCM process guard reused Cobra's ordinary failure exit code 1")
	}
	if got := os.Getenv(key); got != "unchanged" {
		t.Fatalf("legacy SCM invocation changed environment: %s=%q", key, got)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("legacy SCM invocation touched config path %s: %v", configPath, err)
	}
}

func TestMaybeRunWindowsServiceLeavesForegroundInvocationAlone(t *testing.T) {
	originalDetect := detectWindowsService
	detectWindowsService = func() (bool, error) { return false, nil }
	t.Cleanup(func() { detectWindowsService = originalDetect })

	if handled, code := maybeRunWindowsService(); handled || code != 0 {
		t.Fatalf("foreground maybeRunWindowsService = (%t, %d), want (false, 0)", handled, code)
	}
}

func TestMaybeRunWindowsServiceDetectionFailureFailsClosed(t *testing.T) {
	originalDetect := detectWindowsService
	detectWindowsService = func() (bool, error) { return false, errors.New("token query failed") }
	t.Cleanup(func() { detectWindowsService = originalDetect })

	if handled, code := maybeRunWindowsService(); !handled || code != windowsSCMProcessGuardExitCode {
		t.Fatalf(
			"failed detection maybeRunWindowsService = (%t, %d), want (true, %d)",
			handled,
			code,
			windowsSCMProcessGuardExitCode,
		)
	}
}
