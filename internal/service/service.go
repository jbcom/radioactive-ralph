// Package service manages the platform-native auto-restart definition for
// the durable radioactive_ralph supervisor process.
//
// The rewritten runtime (docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md
// §4-§6) is a SINGLE per-user supervisor keyed off the XDG state root, not a
// per-repo daemon — so there is exactly one service definition per user per
// machine, not one per repo. Installing it makes `radioactive_ralph
// --supervisor` a long-running, auto-restarting background process managed
// by the platform's native service host instead of something the operator
// has to remember to start by hand in a terminal.
//
// Platform dispatch:
//
//   - macOS     → launchd user agent
//   - Linux/WSL → systemd user unit
//   - Windows   → native Service Control Manager entry
//
// Service-context detection is used to distinguish durable service
// launches from operator-attached foreground invocations.
package service

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
)

// Backend identifies which platform mechanism is in use.
type Backend string

const (
	// BackendLaunchd is macOS per-user launchd agent.
	BackendLaunchd Backend = "launchd"
	// BackendSystemdUser is Linux/WSL systemd user unit.
	BackendSystemdUser Backend = "systemd-user"
	// BackendWindowsSCM is a native Windows service managed by the Service
	// Control Manager.
	BackendWindowsSCM Backend = "windows-scm"
	// BackendUnsupported is returned for platforms we don't manage.
	BackendUnsupported Backend = "unsupported"
)

// UnitName is the single, stable name for the per-user supervisor service
// definition — there is exactly one per user per machine, so unlike the
// old per-repo scheme this takes no arguments.
//
//	launchd:     "jbcom.radioactive-ralph.supervisor"
//	systemd:     "radioactive_ralph-supervisor"
//	windows-scm: "radioactive_ralph-supervisor"
func UnitName(b Backend) string {
	switch b {
	case BackendLaunchd:
		return "jbcom.radioactive-ralph.supervisor"
	default:
		return "radioactive_ralph-supervisor"
	}
}

// DetectBackend returns the appropriate backend for the current OS.
func DetectBackend() Backend {
	switch runtime.GOOS {
	case "darwin":
		return BackendLaunchd
	case "linux":
		return BackendSystemdUser
	case "windows":
		return BackendWindowsSCM
	default:
		return BackendUnsupported
	}
}

// UnitPath returns the on-disk path where the unit file will be written.
// Callers pass the operator's home dir (tests inject a tmpdir).
func UnitPath(b Backend, home string) string {
	switch b {
	case BackendLaunchd:
		return path.Join(home, "Library", "LaunchAgents", UnitName(b)+".plist")
	case BackendSystemdUser:
		return path.Join(home, ".config", "systemd", "user", UnitName(b)+".service")
	case BackendWindowsSCM:
		return filepath.Join(home, "AppData", "Local", "radioactive-ralph",
			"services", UnitName(b)+".json")
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
	// the unit should exec (with --supervisor). Required.
	RalphBin string
	// ExtraEnv is merged into the unit's environment block. Callers use
	// this for RALPH_STATE_DIR, RALPH_SPEND_CAP_USD, etc.
	ExtraEnv map[string]string
}

// Errors -------------------------------------------------------------

// ErrUnsupportedBackend is returned for platforms we don't manage.
var ErrUnsupportedBackend = errors.New("service: unsupported platform")

// ErrMissingRalphBin is returned when RalphBin is empty.
var ErrMissingRalphBin = errors.New("service: RalphBin required")

// Install writes or registers the platform service definition that runs
// `radioactive_ralph --supervisor` as a per-user auto-restarting background
// process. On launchd/systemd this means writing the unit file; on Windows
// it also registers the SCM entry.
func Install(opts InstallOptions) (path string, err error) {
	if opts.RalphBin == "" {
		return "", ErrMissingRalphBin
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

	path = UnitPath(backend, home)
	// 0o755 — the platform service manager needs directory traversal
	// permission even when running as the same user. 0o750 works on
	// Linux but breaks on macOS where launchd's directory access
	// prechecks expect 0o755 on intermediate dirs.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // service managers require 0o755 intermediate dirs
		return "", fmt.Errorf("service: mkdir %s: %w", filepath.Dir(path), err)
	}

	var content string
	switch backend {
	case BackendLaunchd:
		// launchd's StandardOutPath/StandardErrorPath take a literal
		// filesystem path (no "${HOME}" expansion — see renderLaunchd's
		// doc comment) and launchd does not create missing intermediate
		// directories for them itself, so the log dir must exist before
		// the job is ever bootstrapped or it fails to spawn at all
		// (EX_CONFIG) with nothing written anywhere to explain why.
		logDir := filepath.Join(home, "Library", "Logs", "radioactive-ralph")
		if err := os.MkdirAll(logDir, 0o755); err != nil { //nolint:gosec // matches the 0o755 intermediate-dir requirement above
			return "", fmt.Errorf("service: mkdir log dir %s: %w", logDir, err)
		}
		content = renderLaunchd(opts, home)
	case BackendSystemdUser:
		content = renderSystemdUser(opts)
	case BackendWindowsSCM:
		return installWindowsService(opts, path)
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
	path := UnitPath(backend, home)
	if backend == BackendWindowsSCM {
		return uninstallWindowsService(opts, path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove %s: %w", path, err)
	}
	return nil
}

// IsServiceContext reports whether the current process looks like it's
// running under the durable per-user service host rather than an
// operator-attached foreground invocation.
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

// Status reports whether the per-user supervisor service definition is
// installed. This only inspects the service definition on disk (unit
// file present/absent); it says nothing about whether the supervisor
// process is currently running — callers wanting liveness should combine
// this with supervisor.Find against the XDG state root.
type Status struct {
	Backend   Backend
	Installed bool
	UnitPath  string
}

// Inspect reports the current install status of the per-user supervisor
// service definition for the detected (or overridden) backend.
func Inspect(opts InstallOptions) (Status, error) {
	backend := opts.Backend
	if backend == "" {
		backend = DetectBackend()
	}
	if backend == BackendUnsupported {
		return Status{Backend: backend}, fmt.Errorf("%w: %s", ErrUnsupportedBackend, runtime.GOOS)
	}
	home := opts.HomeDir
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return Status{Backend: backend}, fmt.Errorf("service: user home: %w", err)
		}
		home = h
	}
	path := UnitPath(backend, home)
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return Status{Backend: backend, Installed: true, UnitPath: path}, nil
	case os.IsNotExist(err):
		return Status{Backend: backend, Installed: false, UnitPath: path}, nil
	default:
		return Status{Backend: backend}, fmt.Errorf("service: stat %s: %w", path, err)
	}
}
