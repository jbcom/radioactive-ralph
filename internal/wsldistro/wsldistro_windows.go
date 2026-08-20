//go:build windows

package wsldistro

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

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
