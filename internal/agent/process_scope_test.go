//go:build darwin || linux

package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestReclaimProcessScopeRejectsMalformedInputWithoutEcho(t *testing.T) {
	const opaqueValue = "do-not-echo-scope-value"
	err := ReclaimProcessScope("BAD=KEY", opaqueValue, os.Getpid(), time.Second)
	if err == nil {
		t.Fatal("malformed process scope was accepted")
	}
	if strings.Contains(err.Error(), opaqueValue) {
		t.Fatalf("process scope error echoed opaque value: %q", err)
	}
}

// linuxEnvironReadable reports whether /proc/self/environ is readable for
// processes in other sessions. On GitHub Actions runners (YAMA ptrace_scope=1),
// /proc/PID/environ is EACCES after Setsid(), so the reclaim test cannot
// verify the scope marker. Skip rather than fail on a restricted kernel.
func linuxEnvironReadable(t *testing.T) {
	t.Helper()
	// Only relevant on Linux; Darwin uses sysctl, not /proc.
	if filepath.Separator != '/' {
		return
	}
	self := strconv.Itoa(os.Getpid())
	if _, err := os.ReadFile(filepath.Join("/proc", self, "environ")); err != nil {
		t.Skipf("kernel restricts /proc/PID/environ: %v", err)
	}
}

func TestReclaimProcessScopeKillsSetsidMember(t *testing.T) {
	linuxEnvironReadable(t)
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	const key = "RALPH_TEST_PROCESS_SCOPE"
	const value = "scope-fixture-not-a-secret"
	ready := t.TempDir() + "/ready"
	cmd := exec.Command(testExecutable, "-test.run=^TestProcessScopeSetsidHelper$") //nolint:gosec // test-owned executable
	cmd.Env = append(os.Environ(), key+"="+value,
		"RALPH_TEST_PROCESS_SCOPE_HELPER=1", "RALPH_TEST_PROCESS_SCOPE_READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start process-scope fixture: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	waitForProcessScopeHelper(t, ready)
	if err := ReclaimProcessScope(key, value, os.Getpid(), time.Second); err != nil {
		t.Fatalf("ReclaimProcessScope: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("process-scope fixture exited successfully, want SIGKILL")
	} else if exitErr, ok := err.(*exec.ExitError); !ok || !exitErr.Sys().(syscall.WaitStatus).Signaled() {
		t.Fatalf("process-scope fixture exit = %v, want signal", err)
	}
}

func TestReclaimProcessScopeRequiresExactValue(t *testing.T) {
	linuxEnvironReadable(t)
	testExecutable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	const key = "RALPH_TEST_PROCESS_SCOPE"
	ready := t.TempDir() + "/ready"
	cmd := exec.Command(testExecutable, "-test.run=^TestProcessScopeSetsidHelper$") //nolint:gosec // test-owned executable
	cmd.Env = append(os.Environ(), key+"=exact-value",
		"RALPH_TEST_PROCESS_SCOPE_HELPER=1", "RALPH_TEST_PROCESS_SCOPE_READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start exact-scope fixture: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	waitForProcessScopeHelper(t, ready)
	if err := ReclaimProcessScope(key, "exact-value-suffix", os.Getpid(), time.Second); err != nil {
		t.Fatalf("reclaim nonmatching scope: %v", err)
	}
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("nonmatching scope killed fixture: %v", err)
	}
	if err := ReclaimProcessScope(key, "exact-value", os.Getpid(), time.Second); err != nil {
		t.Fatalf("reclaim exact scope: %v", err)
	}
}

func TestProcessScopeSetsidHelper(t *testing.T) {
	if os.Getenv("RALPH_TEST_PROCESS_SCOPE_HELPER") == "" {
		return
	}
	if _, err := syscall.Setsid(); err != nil {
		t.Fatalf("setsid process-scope fixture: %v", err)
	}
	if err := os.WriteFile(os.Getenv("RALPH_TEST_PROCESS_SCOPE_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatalf("write process-scope readiness: %v", err)
	}
	for {
		time.Sleep(time.Second)
	}
}

func waitForProcessScopeHelper(t *testing.T, ready string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatalf("inspect process-scope readiness: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for process-scope fixture")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
