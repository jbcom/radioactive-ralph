package orch

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
)

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
		strings.HasPrefix(calibration.Model, "ollama/") &&
		strings.TrimSpace(calibration.ModelDigest) == "" {
		return provider.Binding{}, fmt.Errorf("local Ollama calibration requires model_digest")
	}
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
	binaryPath, err := exec.LookPath(binding.Config.Binary)
	if err != nil {
		return provider.Binding{}, fmt.Errorf("resolve calibrated binary: %w", err)
	}
	binaryPath, err = filepath.Abs(binaryPath)
	if err != nil {
		return provider.Binding{}, fmt.Errorf("resolve calibrated binary path: %w", err)
	}
	binaryPath, err = filepath.EvalSymlinks(binaryPath)
	if err != nil {
		return provider.Binding{}, fmt.Errorf("resolve calibrated binary symlinks: %w", err)
	}
	expectedPath, err := filepath.EvalSymlinks(calibration.BinaryPath)
	if err != nil {
		return provider.Binding{}, fmt.Errorf("resolve recorded binary path: %w", err)
	}
	if binaryPath != expectedPath {
		return provider.Binding{}, fmt.Errorf(
			"calibrated binary path changed: expected %s, got %s",
			expectedPath, binaryPath,
		)
	}
	raw, err := os.ReadFile(binaryPath) //nolint:gosec // operator-calibrated executable
	if err != nil {
		return provider.Binding{}, fmt.Errorf("read calibrated binary: %w", err)
	}
	actualHash := fmt.Sprintf("%x", sha256.Sum256(raw))
	if actualHash != calibration.BinarySHA256 {
		return provider.Binding{}, fmt.Errorf(
			"calibrated binary hash changed: expected %s, got %s",
			calibration.BinarySHA256, actualHash,
		)
	}
	return binding, nil
}
