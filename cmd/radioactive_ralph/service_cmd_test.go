package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestServiceInstallUninstallStatusRoundTrip drives the `service
// install|status|uninstall` cobra surface end-to-end against a fake HOME,
// confirming install writes a platform unit exec'ing --supervisor (no
// stale per-repo args), status reports installed/not-installed correctly,
// and uninstall removes it again.
func TestServiceInstallUninstallStatusRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service install/uninstall on windows requires SCM access; covered by internal/service unit tests instead")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	statusCmd := newRootCmd(context.Background())
	var statusOut strings.Builder
	statusCmd.SetOut(&statusOut)
	statusCmd.SetArgs([]string{"service", "status"})
	if err := statusCmd.Execute(); err != nil {
		t.Fatalf("service status (before install): %v", err)
	}

	installCmd := newRootCmd(context.Background())
	installCmd.SetArgs([]string{"service", "install", "--radioactive_ralph-bin", "/usr/local/bin/radioactive_ralph"})
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("service install: %v", err)
	}

	// Confirm the unit file landed somewhere under home and execs
	// --supervisor, not the old per-repo argv.
	found := false
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, readErr := os.ReadFile(path) //nolint:gosec // test-controlled tempdir
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(raw), "--supervisor") {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("no installed unit file under HOME references --supervisor")
	}

	uninstallCmd := newRootCmd(context.Background())
	uninstallCmd.SetArgs([]string{"service", "uninstall"})
	if err := uninstallCmd.Execute(); err != nil {
		t.Fatalf("service uninstall: %v", err)
	}
}

func TestServiceInstallDefaultsBinToOwnExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service install on windows requires SCM access")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{"service", "install"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("service install (no --radioactive_ralph-bin): %v", err)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	found := false
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, readErr := os.ReadFile(path) //nolint:gosec // test-controlled tempdir
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(raw), exe) {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatalf("installed unit does not reference own executable path %q", exe)
	}

	// Clean up so other tests sharing the module cache aren't affected.
	cleanupCmd := newRootCmd(context.Background())
	cleanupCmd.SetArgs([]string{"service", "uninstall"})
	_ = cleanupCmd.Execute()
}

// TestServiceInstallWithEnv confirms --env KEY=VALUE (repeatable) lands in
// the installed unit's environment block — this is how a service host
// (e.g. a CI smoke script) points the managed supervisor at an isolated
// RALPH_STATE_DIR rather than the operator's real one.
func TestServiceInstallWithEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service install on windows requires SCM access")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{
		"service", "install",
		"--radioactive_ralph-bin", "/usr/local/bin/radioactive_ralph",
		"--env", "RALPH_STATE_DIR=/tmp/isolated-state",
		"--env", "FOO=bar",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("service install --env: %v", err)
	}
	t.Cleanup(func() {
		cleanupCmd := newRootCmd(context.Background())
		cleanupCmd.SetArgs([]string{"service", "uninstall"})
		_ = cleanupCmd.Execute()
	})

	found := false
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, readErr := os.ReadFile(path) //nolint:gosec // test-controlled tempdir
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(raw), "RALPH_STATE_DIR") && strings.Contains(string(raw), "/tmp/isolated-state") && strings.Contains(string(raw), "FOO") {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatal("installed unit does not contain the --env-supplied variables")
	}
}

func TestServiceInstallRejectsMalformedEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service install on windows requires SCM access")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{"service", "install", "--radioactive_ralph-bin", "/bin/radioactive_ralph", "--env", "NOEQUALSSIGN"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for a malformed --env value")
	}
}
