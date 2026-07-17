//go:build !windows

package doctor

import "golang.org/x/sys/unix"

// diskFreeBytes returns the free bytes available to an unprivileged user on the
// filesystem containing path, and true on success. On any Statfs error it
// returns (0, false) so the caller treats free space as unknown rather than
// zero — a stat failure must never be reported as "disk full".
func diskFreeBytes(path string) (uint64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, false
	}
	// Bavail is blocks available to non-root; multiply by the fragment/block size.
	//nolint:unconvert // Bsize is int64 on some GOOS, uint32 on others; convert for portability.
	return uint64(st.Bavail) * uint64(st.Bsize), true
}
