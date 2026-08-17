//go:build windows

package adapters

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManagedOpenCodeLauncherRejectsNativeWindowsBeforeProviderStart(t *testing.T) {
	if managedOpenCodeProviderSupported() {
		t.Fatal("native Windows unexpectedly supports managed OpenCode providers")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	marker := filepath.Join(t.TempDir(), "provider-started")
	control := exec.Command(executable, "-test.run=^TestManagedOpenCodeWindowsProviderHelper$") //nolint:gosec // test-owned executable
	control.Env = append(os.Environ(), "RALPH_WINDOWS_PROVIDER_MARKER="+marker)
	if output, err := control.CombinedOutput(); err != nil {
		t.Fatalf("positive provider-start control: %v output=%q", err, output)
	}
	if content, err := os.ReadFile(marker); err != nil || string(content) != "started" { //nolint:gosec // test-owned marker
		t.Fatalf("positive provider-start marker = %q err=%v", content, err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatalf("reset positive provider-start marker: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := RunOpenCodeLauncher(OpenCodeLaunchOptions{
		Context: context.Background(),
		Binary:  executable,
		Args: []string{
			"-test.run=^TestManagedOpenCodeWindowsProviderHelper$",
		},
		Env: []string{
			ManagedSessionEnv + "=managed-session",
			HookEndpointEnv + "=unused-endpoint",
			"RALPH_WINDOWS_PROVIDER_MARKER=" + marker,
		},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if code != 1 {
		t.Fatalf("native Windows managed launch exit = %d, want 1", code)
	}
	if stdout.Len() != 0 || stderr.String() != managedOpenCodeLauncherFailure+"\n" {
		t.Fatalf("native Windows managed output = stdout:%q stderr:%q", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("provider started on unsupported native Windows: %v", err)
	}
}

func TestManagedOpenCodeWindowsProviderHelper(t *testing.T) {
	marker := os.Getenv("RALPH_WINDOWS_PROVIDER_MARKER")
	if marker == "" {
		return
	}
	if err := os.WriteFile(marker, []byte("started"), 0o600); err != nil {
		t.Fatalf("write provider marker: %v", err)
	}
}
