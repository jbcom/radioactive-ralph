package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
	for _, dir := range []string{ralphDir, toolsDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
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

func TestServiceExecutionPathRejectsUntrustedOrMissingDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows ACL trust is not represented by Unix permission bits")
	}
	root := t.TempDir()
	ralphDir := filepath.Join(root, "ralph-bin")
	trustedDir := filepath.Join(root, "trusted-bin")
	worldWritableDir := filepath.Join(root, "world-writable-bin")
	missingDir := filepath.Join(root, "missing-bin")
	for _, dir := range []string{ralphDir, trustedDir, worldWritableDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.Chmod(worldWritableDir, 0o777); err != nil {
		t.Fatalf("chmod %s: %v", worldWritableDir, err)
	}

	got := filepath.SplitList(serviceExecutionPath(
		filepath.Join(ralphDir, "radioactive_ralph"),
		strings.Join([]string{worldWritableDir, missingDir, trustedDir}, string(os.PathListSeparator)),
	))
	joined := strings.Join(got, string(os.PathListSeparator))
	if strings.Contains(joined, worldWritableDir) || strings.Contains(joined, missingDir) {
		t.Fatalf("service PATH retained untrusted entries: %v", got)
	}
	if !strings.Contains(joined, trustedDir) {
		t.Fatalf("service PATH dropped trusted directory %s: %v", trustedDir, got)
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

func TestServiceInstallRejectsInvalidMaxParallelBeforeWritingOrStarting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	startCalls := stubSupervisorServiceStart(t)

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{
		"service", "install",
		"--bin", "/usr/local/bin/radioactive_ralph",
		"--env", maxParallelEnv + "=not-a-number",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), maxParallelEnv+" must be an integer from 1 through") {
		t.Fatalf("service install invalid %s error = %v", maxParallelEnv, err)
	}
	if *startCalls != 0 {
		t.Fatalf("service start calls = %d, want zero after preflight failure", *startCalls)
	}
	status, statusErr := service.Inspect(service.InstallOptions{HomeDir: home})
	if statusErr != nil {
		t.Fatalf("service Inspect: %v", statusErr)
	}
	if status.Installed {
		t.Fatalf("invalid service environment wrote %s before validation", status.UnitPath)
	}
}

func TestServiceInstallDoesNotClaimStartedBeforeEndpointIsReady(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service definition install on Windows requires SCM access")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	startCalls := stubSupervisorServiceStart(t)
	waitSupervisorServiceReady = func(context.Context, string, time.Duration) bool { return false }

	cmd := newRootCmd(context.Background())
	cmd.SetArgs([]string{"service", "install", "--bin", "/usr/local/bin/radioactive_ralph"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("service install readiness error = %v", err)
	}
	if *startCalls != 1 {
		t.Fatalf("service start calls = %d, want one before readiness probe", *startCalls)
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
	originalStart := startSupervisorService
	originalStop := stopSupervisorService
	originalWait := waitSupervisorServiceReady
	calls := 0
	startSupervisorService = func(service.InstallOptions) error {
		calls++
		return nil
	}
	stopSupervisorService = func(service.InstallOptions) error { return nil }
	waitSupervisorServiceReady = func(context.Context, string, time.Duration) bool { return true }
	t.Cleanup(func() {
		startSupervisorService = originalStart
		stopSupervisorService = originalStop
		waitSupervisorServiceReady = originalWait
	})
	return &calls
}
