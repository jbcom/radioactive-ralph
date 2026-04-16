// Package service manages platform service definitions for the durable
// repo-scoped radioactive_ralph runtime.
//
// Platform dispatch:
//
//   - macOS     → launchd user agent
//   - Linux/WSL → systemd user unit
//   - Windows   → native Service Control Manager entry
//
// Service-context detection is used to distinguish durable service
// launches from operator-attached `radioactive_ralph run` sessions.
package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
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

// UnitName returns the canonical service definition name for a repo.
// launchd:     "jbcom.radioactive-ralph.<slug>.<hash>"
// systemd:     "radioactive_ralph-<slug>-<hash>"
// windows-scm: "radioactive_ralph-<slug>-<hash>"
func UnitName(b Backend, repoPath string) string {
	slug, hash := repoToken(repoPath)
	switch b {
	case BackendLaunchd:
		return "jbcom.radioactive-ralph." + slug + "." + hash
	case BackendSystemdUser:
		return "radioactive_ralph-" + slug + "-" + hash
	default:
		return "radioactive_ralph-" + slug + "-" + hash
	}
}

// UnitPath returns the on-disk path where the unit file will be written.
// Callers pass the operator's home dir (tests inject a tmpdir).
func UnitPath(b Backend, home, repoPath string) string {
	switch b {
	case BackendLaunchd:
		return path.Join(home, "Library", "LaunchAgents",
			UnitName(b, repoPath)+".plist")
	case BackendSystemdUser:
		return path.Join(home, ".config", "systemd", "user",
			UnitName(b, repoPath)+".service")
	case BackendWindowsSCM:
		return filepath.Join(home, "AppData", "Local", "radioactive-ralph",
			"services", UnitName(b, repoPath)+".json")
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
	// working directory for the durable runtime and used to derive the
	// service unit name. Required.
	RepoPath string
	// ExtraEnv is merged into the unit's environment block. Callers use
	// this for RALPH_SPEND_CAP_USD etc.
	ExtraEnv map[string]string
}

// Errors -------------------------------------------------------------

// ErrUnsupportedBackend is returned for platforms we don't manage.
var ErrUnsupportedBackend = errors.New("service: unsupported platform")

// ErrMissingRalphBin is returned when RalphBin is empty.
var ErrMissingRalphBin = errors.New("service: RalphBin required")

// ErrMissingRepoPath is returned when RepoPath is empty.
var ErrMissingRepoPath = errors.New("service: RepoPath required")

// Install writes or registers the platform service definition for the
// given repo. On launchd/systemd this means writing the unit file; on
// Windows it also registers the SCM entry.
func Install(opts InstallOptions) (path string, err error) {
	if opts.RalphBin == "" {
		return "", ErrMissingRalphBin
	}
	if opts.RepoPath == "" {
		return "", ErrMissingRepoPath
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

	path = UnitPath(backend, home, opts.RepoPath)
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
		content = renderLaunchd(opts)
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
	if opts.RepoPath == "" {
		return ErrMissingRepoPath
	}
	path := UnitPath(backend, home, opts.RepoPath)
	if backend == BackendWindowsSCM {
		return uninstallWindowsService(opts, path)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove %s: %w", path, err)
	}
	return nil
}

// IsServiceContext reports whether the current process looks like it's
// running under the durable repo-service host rather than an
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

func repoToken(repoPath string) (slug, hash string) {
	base := filepath.Base(strings.TrimSpace(repoPath))
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "repo"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(base) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	slug = strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "repo"
	}
	sum := sha256.Sum256([]byte(repoPath))
	hash = hex.EncodeToString(sum[:])[:10]
	return slug, hash
}
