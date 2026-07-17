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

	"golang.org/x/sys/unix"
)

func listenEndpoint(endpoint string) (net.Listener, error) {
	// Fail with a clear message if even the (short by construction) fallback
	// path still exceeds the kernel's sun_path limit — better than a cryptic
	// "invalid argument" from bind on an exotic system with a very long
	// TMPDIR.
	if len(endpoint) > maxUnixSocketPath {
		return nil, fmt.Errorf("ipc: socket path %q exceeds the %d-byte sun_path limit; set a shorter TMPDIR/RALPH_STATE_DIR", endpoint, maxUnixSocketPath)
	}
	_ = os.Remove(endpoint)
	dir := filepath.Dir(endpoint)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("ipc: mkdir socket parent: %w", err)
	}
	// The too-long-sun_path FALLBACK puts the socket under a Ralph-created
	// "rralph-<uid>" dir in the shared system temp dir, where another user
	// could pre-create or symlink that specific leaf before Ralph starts —
	// MkdirAll neither re-chmods nor verifies ownership of an existing dir.
	// So for OUR fallback leaf only, force it 0700 and verify it is a real
	// directory (not a symlink) owned by us with no group/other access,
	// refusing to bind otherwise. We check only Ralph's own leaf, NOT its
	// shared ancestors (a 0755 /tmp or /var/folders is expected and fine —
	// the 0700 leaf blocks traversal) and NOT the natural XDG-state path
	// (user-private, and legitimately 0755 for ~/.local/state).
	if isFallbackSocketDir(dir) {
		// VERIFY BEFORE MUTATING. A path-based os.Chmod follows symlinks, so
		// chmod'ing before the symlink check would let an attacker who
		// pre-created /tmp/rralph-<uid> as a symlink redirect a
		// victim-privileged chmod onto an arbitrary target dir. verifySecureDir
		// Lstat-rejects a symlink (and a non-dir / other-owned / group-or-
		// world-accessible dir) first; only then do we tighten perms — via an
		// fd opened O_NOFOLLOW so even a TOCTOU swap after the check can't
		// redirect the fchmod through a symlink.
		if err := verifySecureDir(dir); err != nil {
			return nil, fmt.Errorf("ipc: fallback socket dir %s is not safe: %w", dir, err)
		}
		if err := chmodDirNoFollow(dir, 0o700); err != nil {
			return nil, fmt.Errorf("ipc: secure fallback socket dir: %w", err)
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

// isFallbackSocketDir reports whether dir is a Ralph-created too-long-sun_path
// fallback socket dir — the "rralph-<uid>" leaf under the system temp dir
// (see ipc.serviceEndpointForGOOS). Only that leaf, which Ralph owns and
// creates, gets the strict ownership/perms hardening; an arbitrary
// RALPH_STATE_DIR that merely happens to sit under the temp dir must not be
// policed (it is the operator's own choice, not an attacker surface).
func isFallbackSocketDir(dir string) bool {
	tmp := filepath.Clean(os.TempDir())
	d := filepath.Clean(dir)
	if d != tmp && !strings.HasPrefix(d, tmp+string(os.PathSeparator)) {
		return false
	}
	return strings.HasPrefix(filepath.Base(d), "rralph-")
}

// chmodDirNoFollow chmods dir WITHOUT following a symlink: it opens the path
// with O_NOFOLLOW|O_DIRECTORY (so a symlink or non-dir errors out instead of
// being traversed) and fchmods the resulting fd. This closes the TOCTOU
// window a path-based os.Chmod leaves open — even if an attacker swaps dir
// for a symlink between verifySecureDir and here, the open fails rather than
// redirecting the mode change onto the link target.
func chmodDirNoFollow(dir string, mode os.FileMode) error {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	return unix.Fchmod(fd, uint32(mode.Perm()))
}

// verifySecureDir refuses a socket dir an attacker could control on a shared
// host: it must be a real directory (Lstat, so a symlink is rejected — a
// symlink's target is not what we vetted), owned by the current uid, and
// carry no group/other permission bits.
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
