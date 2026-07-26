package orch

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

const calibrationVersionHelperEnv = "RALPH_CALIBRATION_VERSION_HELPER"

func init() {
	if version := os.Getenv(calibrationVersionHelperEnv); version != "" {
		_, _ = fmt.Fprintln(os.Stdout, version)
		os.Exit(0)
	}
}

func TestValidateProviderCalibrationPinsExactAbsoluteExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("calibrated lanes fail closed until Windows Job Object cleanup is implemented")
	}
	binPath, binBytes := writeCalibrationTestBinary(t, "codex", "codex test 1.0")
	t.Setenv("PATH", filepath.Dir(binPath))
	calibration := exactTestCalibration(t, "codex", "gpt-test", "xhigh", binPath, binBytes, "codex test 1.0")

	binding, err := ValidateProviderCalibration(calibration)
	if err != nil {
		t.Fatal(err)
	}
	wantPath, err := filepath.EvalSymlinks(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Config.Binary != wantPath || !filepath.IsAbs(binding.Config.Binary) {
		t.Fatalf("pinned command = %q, want absolute %q", binding.Config.Binary, wantPath)
	}

	retargetedPath, _ := writeCalibrationTestBinary(t, "codex", "codex test 1.0")
	t.Setenv("PATH", filepath.Dir(retargetedPath))
	if _, err := ValidateProviderCalibration(calibration); err == nil ||
		!strings.Contains(err.Error(), "calibrated binary path changed") {
		t.Fatalf("PATH retarget error = %v, want path-change rejection", err)
	}
}

func TestValidateProviderCalibrationRejectsBarePathInvocationHash(t *testing.T) {
	binPath, binBytes := writeCalibrationTestBinary(t, "codex", "codex test 1.0")
	t.Setenv("PATH", filepath.Dir(binPath))
	calibration := exactTestCalibration(
		t, "codex", "gpt-test", "xhigh", binPath, binBytes, "codex test 1.0",
	)
	bareBinding, err := provider.ResolveShippedBinding("codex")
	if err != nil {
		t.Fatal(err)
	}
	bareBinding.Name = calibration.Alias
	calibration.InvocationHash, err = provider.InvocationConfigHash(
		bareBinding, provider.Model(calibration.Model), calibration.Effort,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ValidateProviderCalibration(calibration); err == nil ||
		!strings.Contains(err.Error(), "invocation config hash mismatch") {
		t.Fatalf("bare-path invocation error = %v, want pinned-binding mismatch", err)
	}
}

func TestValidateProviderCalibrationRejectsByteDrift(t *testing.T) {
	binPath, binBytes := writeCalibrationTestBinary(t, "claude", "claude test 1.0")
	t.Setenv("PATH", filepath.Dir(binPath))
	calibration := exactTestCalibration(
		t, "claude", "claude-test-1", "high", binPath, binBytes, "claude test 1.0",
	)
	if err := os.WriteFile(binPath, append(binBytes, byte('\n')), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := ValidateProviderCalibration(calibration); err == nil ||
		!strings.Contains(err.Error(), "calibrated binary hash changed") {
		t.Fatalf("byte-drift error = %v, want hash rejection", err)
	}
}

func TestValidateProviderCalibrationRejectsVersionDrift(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("calibrated lanes fail closed until Windows Job Object cleanup is implemented")
	}
	binPath, binBytes := writeCalibrationTestBinary(t, "codex", "codex test 1.0")
	t.Setenv("PATH", filepath.Dir(binPath))
	calibration := exactTestCalibration(t, "codex", "gpt-test", "xhigh", binPath, binBytes, "codex test 1.0")
	t.Setenv(calibrationVersionHelperEnv, "codex test 2.0")

	if _, err := ValidateProviderCalibration(calibration); err == nil ||
		!strings.Contains(err.Error(), "calibrated binary version changed") {
		t.Fatalf("version-drift error = %v, want version rejection", err)
	}
}

func TestValidateProviderCalibrationRejectsSymlinkRetarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not available to an unprivileged Windows test")
	}
	targetA, bytesA := writeCalibrationTestBinary(t, "target-a", "codex test 1.0")
	targetB, _ := writeCalibrationTestBinary(t, "target-b", "codex test 1.0")
	binDir := t.TempDir()
	link := filepath.Join(binDir, "codex")
	if err := os.Symlink(targetA, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	calibration := exactTestCalibration(t, "codex", "gpt-test", "xhigh", targetA, bytesA, "codex test 1.0")
	if _, err := ValidateProviderCalibration(calibration); err != nil {
		t.Fatalf("initial symlink validation: %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetB, link); err != nil {
		t.Fatal(err)
	}

	if _, err := ValidateProviderCalibration(calibration); err == nil ||
		!strings.Contains(err.Error(), "calibrated binary path changed") {
		t.Fatalf("symlink-retarget error = %v, want path-change rejection", err)
	}
}

func TestCanonicalCalibrationExecutableRejectsNonRegularPath(t *testing.T) {
	if _, err := canonicalCalibrationExecutable(t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("non-regular error = %v", err)
	}
}

func TestCalibratedExecutableSHA256IsBoundedAndExact(t *testing.T) {
	binPath, binBytes := writeCalibrationTestBinary(t, "codex", "codex test 1.0")
	got, err := calibratedExecutableSHA256WithTimeout(binPath, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(binBytes))
	if got != want {
		t.Fatalf("executable sha256 = %s, want %s", got, want)
	}
	if _, err := calibratedExecutableSHA256WithTimeout(binPath, 0); err == nil ||
		!strings.Contains(err.Error(), "timed out") {
		t.Fatalf("zero-bound hash error = %v, want timeout", err)
	}
}

func TestCalibratedBinaryVersionKillsDescendantHoldingOutputPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process-group descendant proof")
	}
	path := filepath.Join(t.TempDir(), "forking-version")
	raw := []byte("#!/bin/sh\n(sleep 30) &\nprintf 'version-before-timeout\\n'\n")
	if err := os.WriteFile(path, raw, 0o755); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err := calibratedBinaryVersionWithTimeout(path, 100*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("forking version probe error = %v, want timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("forking version probe returned after %s, want bounded process-tree cleanup", elapsed)
	}
}

func TestCalibratedBinaryVersionFailsClosedWithoutWindowsJobObject(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only fail-closed contract")
	}
	_, err := calibratedBinaryVersionWithTimeout("unused.exe", time.Second)
	if err == nil || !strings.Contains(err.Error(), "Job Object") {
		t.Fatalf("Windows version-probe error = %v, want Job Object fail-closed reason", err)
	}
}

func TestValidateProviderCalibrationRejectsOpenCodeOllamaUntilAttested(t *testing.T) {
	binding, err := provider.ResolveShippedBinding("opencode")
	if err != nil {
		t.Fatal(err)
	}
	binding.Name = "opencode-qwen"
	invocationHash, err := provider.InvocationConfigHash(
		binding, provider.Model("ollama/qwen3.5:4b"), "default",
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ValidateProviderCalibration(store.ProviderCalibration{
		Alias: "opencode-qwen", Provider: "opencode",
		Model: "ollama/qwen3.5:4b", Effort: "default",
		InvocationHash: invocationHash, ModelDigest: "sha256:not-attested",
		Capabilities: []string{"quality.pixel-composition"},
	})
	if err == nil || !strings.Contains(err.Error(), "endpoint, model, and digest attestation") {
		t.Fatalf("OpenCode Ollama error = %v, want fail-closed attestation rejection", err)
	}
}

func exactTestCalibration(
	t *testing.T,
	providerName, model, effort, binPath string,
	binBytes []byte,
	version string,
) store.ProviderCalibration {
	t.Helper()
	alias := providerName + "-exact-test"
	binding, err := provider.ResolveShippedBinding(providerName)
	if err != nil {
		t.Fatal(err)
	}
	binding.Name = alias
	binding.Config.Binary = binPath
	invocationHash, err := provider.InvocationConfigHash(binding, provider.Model(model), effort)
	if err != nil {
		t.Fatal(err)
	}
	return store.ProviderCalibration{
		Alias: alias, Provider: providerName, Model: model, Effort: effort,
		BinaryPath: binPath, BinaryVersion: version,
		BinarySHA256:    fmt.Sprintf("%x", sha256.Sum256(binBytes)),
		InvocationHash:  invocationHash,
		InferenceDomain: "test-inference", ControlDomain: "local-cli",
		IndependenceDomain: "test-inference",
		Capabilities:       []string{"quality.code-build-test"},
		EvidenceJSON:       `{"fixture":"test"}`,
	}
}

func writeCalibrationTestBinary(t *testing.T, name, version string) (string, []byte) {
	t.Helper()
	var raw []byte
	if runtime.GOOS == "windows" {
		source, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		raw, err = os.ReadFile(source) //nolint:gosec // this package's own test binary
		if err != nil {
			t.Fatal(err)
		}
		name += ".exe"
	} else {
		// Keep the production ten-second version-probe bound strict without
		// making the test boot this package's large test binary under parallel
		// suite load. The tiny helper reads the same test-only sentinel used by
		// the Windows fallback and exits immediately.
		raw = []byte("#!/bin/sh\nprintf '%s\\n' \"$RALPH_CALIBRATION_VERSION_HELPER\"\n")
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, raw, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(calibrationVersionHelperEnv, version)
	return resolved, raw
}
