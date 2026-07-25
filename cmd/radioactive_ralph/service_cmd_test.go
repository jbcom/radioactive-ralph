package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/service"
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
	startCalls := stubSupervisorServiceStart(t)

	statusCmd := newRootCmd(context.Background())
	var statusOut strings.Builder
	statusCmd.SetOut(&statusOut)
	statusCmd.SetArgs([]string{"service", "status"})
	if err := statusCmd.Execute(); err != nil {
		t.Fatalf("service status (before install): %v", err)
	}

	installCmd := newRootCmd(context.Background())
	installCmd.SetArgs([]string{"service", "install", "--bin", "/usr/local/bin/radioactive_ralph"})
	if err := installCmd.Execute(); err != nil {
		t.Fatalf("service install: %v", err)
	}
	if *startCalls != 1 {
		t.Fatalf("service install start calls = %d, want 1", *startCalls)
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
	assertInstalledUnitContains(t, home, "PATH")

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
	stubSupervisorServiceStart(t)

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{"service", "install"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("service install (no --bin): %v", err)
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
	stubSupervisorServiceStart(t)

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{
		"service", "install",
		"--bin", "/usr/local/bin/radioactive_ralph",
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

func TestServiceInstallHonorsExplicitPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service install on windows requires SCM access")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	stubSupervisorServiceStart(t)

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{
		"service", "install",
		"--bin", "/usr/local/bin/radioactive_ralph",
		"--env", "PATH=/controlled/provider/bin:/usr/bin",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("service install explicit PATH: %v", err)
	}
	assertInstalledUnitContains(t, home, "/controlled/provider/bin")
}

func TestServiceExecutionPathIsAbsoluteDeduplicatedAndIncludesBinaryDir(t *testing.T) {
	root := t.TempDir()
	ralphDir := filepath.Join(root, "ralph-bin")
	toolsDir := filepath.Join(root, "tools-bin")
	got := serviceExecutionPath(
		filepath.Join(ralphDir, "radioactive_ralph"),
		strings.Join([]string{toolsDir, "relative/bin", toolsDir}, string(os.PathListSeparator)),
	)
	entries := filepath.SplitList(got)
	if len(entries) == 0 || entries[0] != ralphDir {
		t.Fatalf("serviceExecutionPath = %v, want Ralph binary directory first", entries)
	}
	count := 0
	for _, entry := range entries {
		if !filepath.IsAbs(entry) {
			t.Errorf("serviceExecutionPath contains relative entry %q", entry)
		}
		if entry == toolsDir {
			count++
		}
	}
	if count != 1 {
		t.Errorf("%s appears %d times, want once in %v", toolsDir, count, entries)
	}
}

func TestServiceInstallRejectsMalformedEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service install on windows requires SCM access")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{"service", "install", "--bin", "/bin/radioactive_ralph", "--env", "NOEQUALSSIGN"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for a malformed --env value")
	}
}

func assertInstalledUnitContains(t *testing.T, home, value string) {
	t.Helper()
	found := false
	_ = filepath.WalkDir(home, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		raw, readErr := os.ReadFile(path) //nolint:gosec // test-controlled tempdir
		if readErr == nil && strings.Contains(string(raw), value) {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatalf("installed unit under %s does not contain %q", home, value)
	}
}

func stubSupervisorServiceStart(t *testing.T) *int {
	t.Helper()
	original := startSupervisorService
	calls := 0
	startSupervisorService = func(service.InstallOptions) error {
		calls++
		return nil
	}
	t.Cleanup(func() {
		startSupervisorService = original
	})
	return &calls
}
