//go:build windows

package doctor

import "golang.org/x/sys/windows"

// diskFreeBytes returns the free bytes available to the calling user on the
// volume containing path, and true on success. On any error it returns
// (0, false) so the caller treats free space as unknown rather than zero.
func diskFreeBytes(path string) (uint64, bool) {
	var freeToCaller, total, totalFree uint64
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false
	}
	if err := windows.GetDiskFreeSpaceEx(p, &freeToCaller, &total, &totalFree); err != nil {
		return 0, false
	}
	return freeToCaller, true
}
