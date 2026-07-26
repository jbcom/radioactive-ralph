package orch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

const (
	calibrationVersionTimeout   = 10 * time.Second
	calibrationVersionPipeWait  = 250 * time.Millisecond
	calibrationVersionOutputMax = 64 << 10
	calibrationHashTimeout      = 10 * time.Second
	calibrationHashBufferSize   = 1 << 20
)

var errCalibrationVersionOutputTooLarge = errors.New("calibrated binary version output exceeds 64 KiB")

// ValidateProviderCalibration proves an imported calibration still names the
// exact local binary and invocation configuration it measured.
func ValidateProviderCalibration(calibration store.ProviderCalibration) (provider.Binding, error) {
	binding, err := calibratedProviderBinding(calibration)
	if err != nil {
		return provider.Binding{}, err
	}
	if _, err := provider.ResolveInvocation(binding, provider.Request{
		Model: provider.Model(calibration.Model), Effort: calibration.Effort,
		StrictBinding: true,
	}); err != nil {
		return provider.Binding{}, fmt.Errorf("invalid exact calibration tuple: %w", err)
	}
	for _, capability := range calibration.Capabilities {
		if !provider.CalibrationRequiredCapability(capability) {
			return provider.Binding{}, fmt.Errorf(
				"capability %q is not in the measured calibration vocabulary",
				capability,
			)
		}
	}
	if calibration.Provider == "opencode" &&
		strings.HasPrefix(calibration.Model, "ollama/") {
		return provider.Binding{}, fmt.Errorf(
			"calibrated OpenCode Ollama bindings require endpoint, model, and digest attestation",
		)
	}
	binaryPath, err := exec.LookPath(binding.Config.Binary)
	if err != nil {
		return provider.Binding{}, fmt.Errorf("resolve calibrated binary: %w", err)
	}
	binaryPath, err = canonicalCalibrationExecutable(binaryPath)
	if err != nil {
		return provider.Binding{}, fmt.Errorf("resolve calibrated binary path: %w", err)
	}
	expectedPath, err := canonicalCalibrationExecutable(calibration.BinaryPath)
	if err != nil {
		return provider.Binding{}, fmt.Errorf("resolve recorded binary path: %w", err)
	}
	recordedAbsolute, err := filepath.Abs(calibration.BinaryPath)
	if err != nil {
		return provider.Binding{}, fmt.Errorf("resolve recorded binary path: %w", err)
	}
	if filepath.Clean(recordedAbsolute) != expectedPath {
		return provider.Binding{}, fmt.Errorf(
			"recorded binary path must be absolute and symlink-resolved: got %s, want %s",
			calibration.BinaryPath, expectedPath,
		)
	}
	if binaryPath != expectedPath {
		return provider.Binding{}, fmt.Errorf(
			"calibrated binary path changed: expected %s, got %s",
			expectedPath, binaryPath,
		)
	}
	// The invocation hash must describe the binding the runner actually
	// receives. Pin the canonical executable before hashing so a record created
	// from a bare PATH lookup cannot claim the absolute-path execution lane.
	binding.Config.Binary = binaryPath
	invocationHash, err := provider.InvocationConfigHash(
		binding, provider.Model(calibration.Model), calibration.Effort,
	)
	if err != nil {
		return provider.Binding{}, err
	}
	if invocationHash != calibration.InvocationHash {
		return provider.Binding{}, fmt.Errorf(
			"invocation config hash mismatch: expected %s, got %s",
			calibration.InvocationHash, invocationHash,
		)
	}
	actualHash, err := calibratedExecutableSHA256(binaryPath)
	if err != nil {
		return provider.Binding{}, fmt.Errorf("hash calibrated binary: %w", err)
	}
	if actualHash != calibration.BinarySHA256 {
		return provider.Binding{}, fmt.Errorf(
			"calibrated binary hash changed: expected %s, got %s",
			calibration.BinarySHA256, actualHash,
		)
	}
	actualVersion, err := calibratedBinaryVersion(binaryPath)
	if err != nil {
		return provider.Binding{}, fmt.Errorf("probe calibrated binary version: %w", err)
	}
	expectedVersion := normalizeCalibrationVersion(calibration.BinaryVersion)
	if actualVersion != expectedVersion {
		return provider.Binding{}, fmt.Errorf(
			"calibrated binary version changed: expected %q, got %q",
			expectedVersion, actualVersion,
		)
	}
	return binding, nil
}

func canonicalCalibrationExecutable(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", resolved)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", resolved)
	}
	return resolved, nil
}

func calibratedBinaryVersion(binaryPath string) (string, error) {
	return calibratedBinaryVersionWithTimeout(binaryPath, calibrationVersionTimeout)
}

func calibratedBinaryVersionWithTimeout(binaryPath string, timeout time.Duration) (string, error) {
	if runtime.GOOS == "windows" {
		return "", fmt.Errorf(
			"calibrated version probes are unavailable on Windows until Job Object descendant cleanup is implemented",
		)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var output boundedCalibrationVersionOutput
	cmd := exec.CommandContext(ctx, binaryPath, "--version") //nolint:gosec // exact operator-calibrated executable
	cmd.Stdin = nil
	cmd.Stdout = &output
	cmd.Stderr = &output
	provider.ConfigureProcessCancellation(cmd)
	// A short-lived version command must not leave a descendant holding its
	// inherited output pipe. os/exec does not call Cancel after the direct
	// child has already exited, so bound that drain separately and explicitly
	// kill the process group below on timeout or ErrWaitDelay.
	cmd.WaitDelay = calibrationVersionPipeWait
	err := cmd.Run()
	if ctx.Err() != nil || errors.Is(err, exec.ErrWaitDelay) {
		_ = provider.KillProcessTree(cmd.Process)
	}
	if ctx.Err() != nil {
		return "", fmt.Errorf("version probe timed out after %s: %w", timeout, ctx.Err())
	}
	if errors.Is(output.err, errCalibrationVersionOutputTooLarge) {
		return "", output.err
	}
	if err != nil {
		return "", fmt.Errorf("%s --version: %w: %s", binaryPath, err, normalizeCalibrationVersion(output.String()))
	}
	return normalizeCalibrationVersion(output.String()), nil
}

func calibratedExecutableSHA256(binaryPath string) (string, error) {
	return calibratedExecutableSHA256WithTimeout(binaryPath, calibrationHashTimeout)
}

func calibratedExecutableSHA256WithTimeout(
	binaryPath string,
	timeout time.Duration,
) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("executable hash timed out after %s: %w", timeout, err)
	}
	file, err := os.Open(binaryPath) //nolint:gosec // exact operator-calibrated executable
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	type hashResult struct {
		digest string
		err    error
	}
	result := make(chan hashResult, 1)
	go func() {
		digest := sha256.New()
		_, copyErr := io.CopyBuffer(digest, file, make([]byte, calibrationHashBufferSize))
		result <- hashResult{digest: fmt.Sprintf("%x", digest.Sum(nil)), err: copyErr}
	}()

	select {
	case hashed := <-result:
		return hashed.digest, hashed.err
	case <-ctx.Done():
		_ = file.Close()
		return "", fmt.Errorf("executable hash timed out after %s: %w", timeout, ctx.Err())
	}
}

func normalizeCalibrationVersion(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.TrimSpace(value)
}

type boundedCalibrationVersionOutput struct {
	buf bytes.Buffer
	err error
}

func (w *boundedCalibrationVersionOutput) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	remaining := calibrationVersionOutputMax - w.buf.Len()
	if len(p) > remaining {
		if remaining > 0 {
			_, _ = w.buf.Write(p[:remaining])
		}
		w.err = errCalibrationVersionOutputTooLarge
		return remaining, w.err
	}
	return w.buf.Write(p)
}

func (w *boundedCalibrationVersionOutput) String() string {
	return w.buf.String()
}
