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
	// Bavail (blocks available to non-root) is uint64 on every supported GOOS.
	// Bsize is int64 on Linux but uint32 on darwin/BSD; normalize it through int64
	// and reject a negative value (a real filesystem never reports one) before the
	// unsigned multiply, so gosec's G115 overflow check is satisfied and a bogus
	// negative can't wrap to a huge "free" figure — treat that as unknown instead.
	bsize := int64(st.Bsize) //nolint:unconvert // Bsize is uint32 on darwin/BSD (conversion needed) but int64 on Linux (where it reads as redundant)
	if bsize < 0 {
		return 0, false
	}
	return st.Bavail * uint64(bsize), true
}
