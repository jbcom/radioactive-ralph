//go:build windows

package wsldistro

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	wsl "github.com/ubuntu/gowsl"
)

// DistroName is the dedicated distro's registered name. Kept in sync with
// internal/agent.RalphWSLDistroName by convention, not by import, to avoid a
// dependency cycle between internal/agent and internal/wsldistro -- both
// packages are leaves that the future auto-provisioning call site (not yet
// wired) will need to agree on.
const DistroName = "radioactive-ralph"

// Check reports whether wsl.exe is available and the managed distro is
// already registered. Never registers anything itself -- see Register for
// that, called separately (and not yet wired into any command path; see the
// design spec's open questions on when provisioning should happen).
func Check(ctx context.Context) Status {
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		return Status{
			Applicable:   true,
			WSLAvailable: false,
			Detail:       "wsl.exe not found on PATH; install WSL2",
		}
	}

	registered, err := isRegistered(ctx)
	if err != nil {
		return Status{
			Applicable:   true,
			WSLAvailable: true,
			Detail:       fmt.Sprintf("wsl.exe found, but could not enumerate distros: %v", err),
		}
	}
	if registered {
		return Status{
			Applicable:       true,
			WSLAvailable:     true,
			DistroRegistered: true,
			Detail:           fmt.Sprintf("%q distro registered", DistroName),
		}
	}
	return Status{
		Applicable:   true,
		WSLAvailable: true,
		Detail:       fmt.Sprintf("wsl.exe found, %q distro not yet registered", DistroName),
	}
}

func isRegistered(ctx context.Context) (bool, error) {
	distros, err := wsl.RegisteredDistros(ctx)
	if err != nil {
		return false, err
	}
	for _, d := range distros {
		if d.Name() == DistroName {
			return true, nil
		}
	}
	return false, nil
}

// ErrAlreadyRegistered is returned by Register when the distro already
// exists -- matching gowsl.Distro.Register's own "already registered" error
// text, wrapped so callers can errors.Is against a stable value instead of
// string-matching.
var ErrAlreadyRegistered = errors.New("wsldistro: distro already registered")

// Register imports the distro from rootFsPath (produced by
// packaging/wsl/build-rootfs.sh). Not yet called from any command path --
// see the design spec's open question on when provisioning should happen
// (first native-Windows dispatch attempt vs. eagerly at --init).
func Register(ctx context.Context, rootFsPath string) error {
	registered, err := isRegistered(ctx)
	if err != nil {
		return fmt.Errorf("wsldistro: check existing registration: %w", err)
	}
	if registered {
		return ErrAlreadyRegistered
	}

	d := wsl.NewDistro(ctx, DistroName)
	if err := d.Register(rootFsPath); err != nil {
		return fmt.Errorf("wsldistro: register %q from %s: %w", DistroName, rootFsPath, err)
	}
	return nil
}

// ErrRootfsNotBundled is returned by EnsureRegistered when no rootfs.tar.gz
// is found alongside the running executable. Distinct from a Register
// failure: this means the release artifact itself is missing, not that
// registration was attempted and failed.
var ErrRootfsNotBundled = errors.New("wsldistro: rootfs.tar.gz not found next to the running executable")

// bundledRootfsPath returns the expected location of the release-bundled
// rootfs: next to the running executable, matching the convention this
// design already follows for other Windows-bundled binaries (see the
// (abandoned) ConPTY investigation's OpenConsole.exe/conpty.dll research in
// the design spec) -- release packaging places companion files beside the
// main binary, not in some separate, harder-to-locate directory.
func bundledRootfsPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("wsldistro: resolve running executable path: %w", err)
	}
	return filepath.Join(filepath.Dir(exe), "rootfs.tar.gz"), nil
}

// EnsureRegistered registers the managed distro from the release-bundled
// rootfs if it is not already registered, and is a fast no-op if it is.
// This is the actual auto-provisioning entry point -- unlike Check/Register
// above, which only report/act on explicit request, this is what
// pty_start_windows.go calls before a dispatch attempt so a first-time
// Windows user does not have to run a separate setup step by hand.
//
// Deliberately does NOT download anything over the network: the rootfs is
// a real release artifact (packaging/wsl/build-rootfs.sh, ~68MB), and an
// silent first-run network fetch of that size, from inside a provider
// turn's dispatch path, is not a decision to make implicitly. If the
// bundled file is missing, this fails with ErrRootfsNotBundled and a clear
// remediation string rather than reaching out to fetch one.
func EnsureRegistered(ctx context.Context) error {
	registered, err := isRegistered(ctx)
	if err != nil {
		return fmt.Errorf("wsldistro: check existing registration: %w", err)
	}
	if registered {
		return nil
	}

	rootfsPath, err := bundledRootfsPath()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(rootfsPath); statErr != nil {
		return fmt.Errorf("%w (expected at %s): %w", ErrRootfsNotBundled, rootfsPath, statErr)
	}

	if err := Register(ctx, rootfsPath); err != nil && !errors.Is(err, ErrAlreadyRegistered) {
		return err
	}
	return nil
}
