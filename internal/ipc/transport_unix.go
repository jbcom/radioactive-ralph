//go:build !windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func listenEndpoint(endpoint string) (net.Listener, error) {
	_ = os.Remove(endpoint)
	dir := filepath.Dir(endpoint)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("ipc: mkdir socket parent: %w", err)
	}
	// The too-long-sun_path FALLBACK puts the socket parent under the shared
	// system temp dir, where another user could pre-create or symlink it
	// before Ralph starts (MkdirAll neither fixes perms nor verifies
	// ownership of an existing dir). For that case only, verify the parent is
	// a real directory (not a symlink), owned by us, with no group/other
	// access — refuse to bind otherwise. The natural path lives under the
	// user's own XDG state root (not shared), so it is not subject to this
	// stricter check, which would otherwise reject a legitimately 0755
	// ~/.local/state.
	if underTempDir(dir) {
		if err := verifySecureDir(dir); err != nil {
			return nil, fmt.Errorf("ipc: fallback socket parent %s is not safe: %w", dir, err)
		}
	}
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(endpoint, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("ipc: chmod socket: %w", err)
	}
	return listener, nil
}

// underTempDir reports whether dir is inside the system temp dir — i.e. the
// too-long-sun_path fallback location, which is shared and needs the strict
// ownership/perms check that the user-private XDG state root does not.
func underTempDir(dir string) bool {
	tmp := filepath.Clean(os.TempDir())
	d := filepath.Clean(dir)
	return d == tmp || strings.HasPrefix(d, tmp+string(os.PathSeparator))
}

// verifySecureDir refuses a socket-parent directory that an attacker could
// control on a shared host: it must be a real directory (Lstat, so a symlink
// is rejected — a symlink's target is not what we vetted), owned by the
// current uid, and carry no group/other permission bits.
func verifySecureDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("is a symlink")
	}
	if !fi.IsDir() {
		return fmt.Errorf("is not a directory")
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("is group/world-accessible (mode %o)", fi.Mode().Perm())
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		if int(st.Uid) != os.Getuid() {
			return fmt.Errorf("is owned by uid %d, not %d", st.Uid, os.Getuid())
		}
	}
	return nil
}

func cleanupEndpoint(endpoint string) error {
	return os.Remove(endpoint)
}

func dialEndpoint(ctx context.Context, endpoint string, timeout time.Duration) (net.Conn, error) {
	dialer := net.Dialer{Timeout: timeout}
	return dialer.DialContext(ctx, "unix", endpoint)
}
