// Package service manages per-user OS service units for radioactive-ralph.
//
// Platform dispatch:
//
//   - macOS     → launchd user agent (~/Library/LaunchAgents/jbcom.radioactive-ralph.<variant>.plist)
//   - Linux/WSL → systemd user unit (~/.config/systemd/user/radioactive_ralph-<variant>.service)
//   - Homebrew  → brew-services wrapper (invokes the launchd/systemd path)
//
// Gating:
//
//   - Variants with SafetyFloors.RefuseServiceContext = true refuse to
//     install. Running savage/old-man/world-breaker under a service
//     manager is operator malpractice (they burn money or force-push
//     repos; neither should be on a cron).
//   - Variants with a confirmation gate require the operator to have
//     passed the gate flag to `radioactive_ralph service install` explicitly.
//
// Service-context detection at `radioactive_ralph run` time uses:
//
//   - LAUNCHED_BY=launchd     (our own plist sets this)
//   - INVOCATION_ID set        (systemd user services set this)
//   - RALPH_SERVICE_CONTEXT=1  (manual override for tests)
//
// Supervisor refuses to spawn a RefuseServiceContext variant when any
// of those are set.
package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// Backend identifies which platform mechanism is in use.
type Backend string

const (
	// BackendLaunchd is macOS per-user launchd agent.
	BackendLaunchd Backend = "launchd"
	// BackendSystemdUser is Linux/WSL systemd user unit.
	BackendSystemdUser Backend = "systemd-user"
	// BackendUnsupported is returned for platforms we don't manage.
	BackendUnsupported Backend = "unsupported"
)

// DetectBackend returns the appropriate backend for the current OS.
func DetectBackend() Backend {
	switch runtime.GOOS {
	case "darwin":
		return BackendLaunchd
	case "linux":
		return BackendSystemdUser
	default:
		return BackendUnsupported
	}
}

// UnitName returns the canonical service unit name for a variant.
// launchd: "jbcom.radioactive-ralph.green"
// systemd: "radioactive_ralph-green"
func UnitName(b Backend, v variant.Name) string {
	switch b {
	case BackendLaunchd:
		return "jbcom.radioactive-ralph." + string(v)
	case BackendSystemdUser:
		return "radioactive_ralph-" + string(v)
	default:
		return "radioactive_ralph-" + string(v)
	}
}

// UnitPath returns the on-disk path where the unit file will be written.
// Callers pass the operator's home dir (tests inject a tmpdir).
func UnitPath(b Backend, home string, v variant.Name) string {
	switch b {
	case BackendLaunchd:
		return filepath.Join(home, "Library", "LaunchAgents",
			UnitName(b, v)+".plist")
	case BackendSystemdUser:
		return filepath.Join(home, ".config", "systemd", "user",
			UnitName(b, v)+".service")
	default:
		return ""
	}
}

// InstallOptions configures an install.
type InstallOptions struct {
	// Backend overrides the detected platform. Empty = detect.
	Backend Backend
	// HomeDir overrides os.UserHomeDir. Empty = use os.UserHomeDir().
	HomeDir string
	// RalphBin is the absolute path to the radioactive_ralph binary that
	// the unit should exec. Required.
	RalphBin string
	// RepoPath is the operator's repo — written into the unit as the
	// working directory for the daemon.
	RepoPath string
	// Variant is the variant profile to install for. Required.
	Variant variant.Profile
	// GateConfirmed must be true when Variant has a ConfirmationGate.
	// Enforces "operator explicitly passed --confirm-X to
	// radioactive_ralph service install" so gates aren't bypassed via
	// the service wrapper.
	GateConfirmed bool
	// ExtraEnv is merged into the unit's environment block. Callers use
	// this for RALPH_SPEND_CAP_USD etc.
	ExtraEnv map[string]string
}

// Errors -------------------------------------------------------------

// ErrRefuseServiceContext is returned when the variant pins
// RefuseServiceContext=true.
var ErrRefuseServiceContext = errors.New("service: variant refuses to run in a service context")

// ErrGateNotConfirmed is returned when a gated variant is installed
// without GateConfirmed=true.
var ErrGateNotConfirmed = errors.New("service: gated variant requires explicit confirmation")

// ErrUnsupportedBackend is returned for platforms we don't manage.
var ErrUnsupportedBackend = errors.New("service: unsupported platform")

// ErrMissingRalphBin is returned when RalphBin is empty.
var ErrMissingRalphBin = errors.New("service: RalphBin required")

// Install writes the unit file for the given variant.
// Does not load it into launchd/systemd — callers do that via
// `radioactive_ralph service start` to keep Install a pure filesystem
// operation (trivial to test and to undo).
func Install(opts InstallOptions) (path string, err error) {
	if opts.RalphBin == "" {
		return "", ErrMissingRalphBin
	}
	if opts.Variant.SafetyFloors.RefuseServiceContext {
		return "", fmt.Errorf("%w: %s", ErrRefuseServiceContext, opts.Variant.Name)
	}
	if opts.Variant.HasGate() && !opts.GateConfirmed {
		return "", fmt.Errorf("%w: %s requires %s",
			ErrGateNotConfirmed, opts.Variant.Name, opts.Variant.ConfirmationGate)
	}

	backend := opts.Backend
	if backend == "" {
		backend = DetectBackend()
	}
	if backend == BackendUnsupported {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedBackend, runtime.GOOS)
	}

	home := opts.HomeDir
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("service: user home: %w", err)
		}
		home = h
	}

	path = UnitPath(backend, home, opts.Variant.Name)
	// 0o755 — the service manager (launchd on macOS, systemd-user on Linux)
	// needs directory traversal permission even when running as the same
	// user. 0o750 works on Linux but breaks on macOS where launchd's
	// directory access prechecks expect 0o755 on intermediate dirs.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // service managers require 0o755 intermediate dirs
		return "", fmt.Errorf("service: mkdir %s: %w", filepath.Dir(path), err)
	}

	var content string
	switch backend {
	case BackendLaunchd:
		content = renderLaunchd(opts)
	case BackendSystemdUser:
		content = renderSystemdUser(opts)
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedBackend, backend)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // unit file must be readable by service manager
		return "", fmt.Errorf("service: write %s: %w", path, err)
	}
	return path, nil
}

// Uninstall removes the unit file. Returns nil if already absent.
func Uninstall(opts InstallOptions) error {
	backend := opts.Backend
	if backend == "" {
		backend = DetectBackend()
	}
	if backend == BackendUnsupported {
		return fmt.Errorf("%w: %s", ErrUnsupportedBackend, runtime.GOOS)
	}
	home := opts.HomeDir
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("service: user home: %w", err)
		}
		home = h
	}
	path := UnitPath(backend, home, opts.Variant.Name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove %s: %w", path, err)
	}
	return nil
}

// IsServiceContext reports whether the current process looks like it's
// running under a service manager (launchd / systemd --user). Checked
// in pre-flight before spawning a RefuseServiceContext variant.
func IsServiceContext() bool {
	if os.Getenv("RALPH_SERVICE_CONTEXT") == "1" {
		return true
	}
	// Our own launchd plist sets LAUNCHED_BY=launchd.
	if os.Getenv("LAUNCHED_BY") == "launchd" {
		return true
	}
	// systemd --user services always have INVOCATION_ID.
	if os.Getenv("INVOCATION_ID") != "" {
		return true
	}
	return false
}
