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
//   - Windows   → foreground control plane only; native SCM install/start is
//     intentionally disabled until it can preserve Ralph's per-user authority
//
// Service-context detection is used to distinguish durable service
// launches from operator-attached foreground invocations.
package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// execCommand runs a command and returns its combined output. A package var so
// tests can stub the service-manager calls (launchctl/systemctl) without a
// real daemon.
var execCommand = func(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput() //nolint:gosec // fixed service-manager argv, not user input
	return string(out), err
}

var serviceRetrySleep = time.Sleep

const (
	launchdBootstrapAttempts = 30
	launchdBootstrapDelay    = 100 * time.Millisecond
)

// Backend identifies which platform mechanism is in use.
type Backend string

const (
	// BackendLaunchd is macOS per-user launchd agent.
	BackendLaunchd Backend = "launchd"
	// BackendSystemdUser is Linux/WSL systemd user unit.
	BackendSystemdUser Backend = "systemd-user"
	// BackendWindowsSCM identifies a legacy native Windows Service Control
	// Manager registration. Install and Start reject this backend; Inspect,
	// Stop, and Uninstall retain remediation access to older registrations.
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

// ErrInvalidRalphBin is returned when the service executable is not an
// absolute, NUL-free path. Service managers do not perform shell PATH lookup,
// so accepting a relative command would create a definition that cannot be
// reconciled predictably.
var ErrInvalidRalphBin = errors.New("service: RalphBin must be an absolute path")

// ErrWindowsSCMDisabled is the stable sentinel wrapped by
// WindowsSCMDisabledError. Native Windows can run the foreground control
// plane, but the legacy LocalSystem SCM design cannot safely represent Ralph's
// per-user state, credentials, repositories, and control-pipe authority.
var ErrWindowsSCMDisabled = errors.New("service: native Windows SCM is disabled")

// ErrWindowsSCMDeletionPending is the stable sentinel wrapped by
// WindowsSCMDeletionPendingError. A service marked for deletion is not proven
// absent: SCM can retain both its registration and process until it stops and
// every open handle closes.
var ErrWindowsSCMDeletionPending = errors.New("service: native Windows SCM service deletion is pending")

// WindowsSCMOperation identifies the rejected mutating SCM operation.
type WindowsSCMOperation string

const (
	// WindowsSCMOperationInstall is service registration/configuration.
	WindowsSCMOperationInstall WindowsSCMOperation = "installation"
	// WindowsSCMOperationStart is starting a prior registration.
	WindowsSCMOperationStart WindowsSCMOperation = "start"
	// WindowsSCMOperationExecute is an SCM-hosted legacy process invocation.
	WindowsSCMOperationExecute WindowsSCMOperation = "execution"
)

// WindowsSCMDisabledError is returned whenever code tries to install, start,
// or execute the disabled native Windows SCM integration. Callers may use
// errors.As for the operation and errors.Is(err, ErrWindowsSCMDisabled) for
// the stable category.
type WindowsSCMDisabledError struct {
	Operation WindowsSCMOperation
}

func (e *WindowsSCMDisabledError) Error() string {
	operation := e.Operation
	if operation == "" {
		operation = "operation"
	}
	return fmt.Sprintf(
		"service: native Windows SCM service %s is disabled; run radioactive_ralph --supervisor "+
			"in the foreground for the native control plane, or use WSL2 with systemd --user "+
			"for durable provider-backed execution",
		operation,
	)
}

// Unwrap makes the typed error compatible with errors.Is.
func (e *WindowsSCMDisabledError) Unwrap() error {
	return ErrWindowsSCMDisabled
}

// NewWindowsSCMDisabledError constructs the exported typed rejection used by
// the service package and the process-entry guard.
func NewWindowsSCMDisabledError(operation WindowsSCMOperation) error {
	return &WindowsSCMDisabledError{Operation: operation}
}

// WindowsSCMDeletionPendingError reports that a legacy Ralph SCM registration
// is marked for deletion but cannot yet be proven stopped and absent. Callers
// may use errors.Is(err, ErrWindowsSCMDeletionPending) for the stable category.
type WindowsSCMDeletionPendingError struct {
	ServiceName string
	Operation   string
}

func (e *WindowsSCMDeletionPendingError) Error() string {
	serviceName := e.ServiceName
	if serviceName == "" {
		serviceName = UnitName(BackendWindowsSCM)
	}
	operation := e.Operation
	if operation == "" {
		operation = "remediation"
	}
	return fmt.Sprintf(
		"service: native Windows SCM service %q is marked for deletion during %s; "+
			"its process and registration may still exist; wait for SCM deletion to finish "+
			"or reboot Windows, then retry the remediation command",
		serviceName,
		operation,
	)
}

// Unwrap makes the typed error compatible with errors.Is.
func (e *WindowsSCMDeletionPendingError) Unwrap() error {
	return ErrWindowsSCMDeletionPending
}

// Install writes or registers the platform service definition that runs
// `radioactive_ralph --supervisor` as a per-user auto-restarting background
// process. On launchd/systemd this means writing the unit file. Native Windows
// SCM installation is intentionally rejected before any filesystem or SCM
// mutation.
func Install(opts InstallOptions) (path string, err error) {
	backend := opts.Backend
	if backend == "" {
		backend = DetectBackend()
	}
	if backend == BackendWindowsSCM {
		return "", NewWindowsSCMDisabledError(WindowsSCMOperationInstall)
	}
	if backend == BackendUnsupported {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedBackend, runtime.GOOS)
	}
	if opts.RalphBin == "" {
		return "", ErrMissingRalphBin
	}
	if !isAbsoluteServicePath(backend, opts.RalphBin) || strings.ContainsRune(opts.RalphBin, '\x00') {
		return "", fmt.Errorf("%w: %q", ErrInvalidRalphBin, opts.RalphBin)
	}
	for key, value := range opts.ExtraEnv {
		if !validEnvName(key) {
			return "", fmt.Errorf("service: invalid environment variable name %q", key)
		}
		if strings.ContainsRune(value, '\x00') || strings.ContainsAny(value, "\r\n") {
			return "", fmt.Errorf("service: environment variable %s contains a forbidden NUL or newline", key)
		}
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
	default:
		return "", fmt.Errorf("%w: %s", ErrUnsupportedBackend, backend)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // unit file must be readable by service manager
		return "", fmt.Errorf("service: write %s: %w", path, err)
	}
	return path, nil
}

// isAbsoluteServicePath validates the executable using the target service
// manager's path grammar, not the host that happens to render or test the
// definition. Backend overrides are intentionally supported for cross-platform
// launchd/systemd artifact tests, so filepath.IsAbs alone would reject a valid
// POSIX path on Windows.
func isAbsoluteServicePath(backend Backend, value string) bool {
	switch backend {
	case BackendLaunchd, BackendSystemdUser:
		return strings.HasPrefix(value, "/")
	default:
		return false
	}
}

func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// Uninstall removes the platform service definition. It does not stop a
// running service; callers performing the operator-facing uninstall
// lifecycle must call Stop first. Keeping definition removal separate makes
// render/install tests independent of a live service manager while the CLI
// composes the complete stop-then-remove operation.
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
	if backend == BackendSystemdUser {
		if out, err := runService("systemctl", "--user", "daemon-reload"); err != nil {
			return fmt.Errorf("service: systemctl daemon-reload after remove: %w\n%s", err, out)
		}
	}
	return nil
}

// Start loads/starts the installed per-user supervisor service so its process
// actually comes up. Install only WRITES the unit definition; on launchd and
// systemd the unit must additionally be loaded/started (a launchd unit with
// RunAtLoad still needs `launchctl bootstrap`; systemd needs `systemctl
// --user start`). Native Windows SCM start is intentionally rejected before
// any filesystem, SCM, or process access.
func Start(opts InstallOptions) error {
	backend := opts.Backend
	if backend == "" {
		backend = DetectBackend()
	}
	if backend == BackendWindowsSCM {
		return NewWindowsSCMDisabledError(WindowsSCMOperationStart)
	}
	home := opts.HomeDir
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("service: user home: %w", err)
		}
		home = h
	}

	switch backend {
	case BackendLaunchd:
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		label := UnitName(backend)
		path := UnitPath(backend, home)
		// Reload the definition on every install/start. launchd caches a loaded
		// plist, so bootstrapping over an existing label returns EIO and leaves
		// old ProgramArguments/environment active. bootout is intentionally
		// best-effort: a first install has nothing loaded yet. The bootout
		// transition is asynchronous on macOS: even after `launchctl print`
		// stops finding the label, an immediate bootstrap can transiently return
		// exit 5/EIO. Retry that bounded transition instead of requiring the
		// operator to rerun service install.
		_, _ = runService("launchctl", "bootout", domain+"/"+label)
		if out, err := bootstrapLaunchd(domain, path); err != nil {
			return fmt.Errorf("service: launchctl bootstrap: %w\n%s", err, out)
		}
		// RunAtLoad + KeepAlive make bootstrap itself the start operation. A
		// following `kickstart -k` only kills the process bootstrap just created,
		// adding a needless restart and another launchd race.
		return nil
	case BackendSystemdUser:
		unit := UnitName(backend)
		if out, err := runService("systemctl", "--user", "daemon-reload"); err != nil {
			return fmt.Errorf("service: systemctl daemon-reload: %w\n%s", err, out)
		}
		if out, err := runService("systemctl", "--user", "enable", unit); err != nil {
			return fmt.Errorf("service: systemctl enable: %w\n%s", err, out)
		}
		if out, err := runService("systemctl", "--user", "restart", unit); err != nil {
			return fmt.Errorf("service: systemctl restart: %w\n%s", err, out)
		}
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedBackend, backend)
	}
}

// Stop unloads/stops the installed service without removing its definition.
// It is idempotent when the service is not loaded. The operator-facing
// `service uninstall` command calls Stop before Uninstall so KeepAlive or
// enabled services cannot survive after their definition is deleted.
func Stop(opts InstallOptions) error {
	backend := opts.Backend
	if backend == "" {
		backend = DetectBackend()
	}

	switch backend {
	case BackendLaunchd:
		domainTarget := fmt.Sprintf("gui/%d/%s", os.Getuid(), UnitName(backend))
		if _, err := runService("launchctl", "print", domainTarget); err != nil {
			return nil
		}
		if out, err := runService("launchctl", "bootout", domainTarget); err != nil {
			return fmt.Errorf("service: launchctl bootout: %w\n%s", err, out)
		}
		return nil
	case BackendSystemdUser:
		unit := UnitName(backend)
		out, err := runService("systemctl", "--user", "show", unit, "--property=LoadState", "--value")
		if err == nil && strings.TrimSpace(out) != "not-found" {
			if disableOut, disableErr := runService("systemctl", "--user", "disable", "--now", unit); disableErr != nil {
				return fmt.Errorf("service: systemctl disable --now: %w\n%s", disableErr, disableOut)
			}
		}
		return nil
	case BackendWindowsSCM:
		return stopWindowsService(opts)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedBackend, backend)
	}
}

func bootstrapLaunchd(domain, unitPath string) (string, error) {
	var lastOut string
	var lastErr error
	for attempt := 1; attempt <= launchdBootstrapAttempts; attempt++ {
		lastOut, lastErr = runService("launchctl", "bootstrap", domain, unitPath)
		if lastErr == nil {
			return lastOut, nil
		}
		if attempt < launchdBootstrapAttempts {
			serviceRetrySleep(launchdBootstrapDelay)
		}
	}
	return lastOut, lastErr
}

// runService runs a service-manager command, returning combined output for
// error diagnostics. Split out so tests can observe it and the callers stay
// readable.
func runService(name string, args ...string) (string, error) {
	out, err := execCommand(name, args...)
	return out, err
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
// installed. launchd/systemd inspect the unit definition on disk. Native
// Windows asks SCM directly so a registration left by an earlier development
// build remains discoverable even when its legacy JSON config is absent.
// It says nothing about whether the supervisor process is currently running —
// callers wanting liveness should combine this with supervisor.Find against
// the XDG state root.
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
	if backend == BackendWindowsSCM {
		return inspectWindowsService(opts)
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
