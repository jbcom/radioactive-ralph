//go:build !windows

package adapters

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// cleanupOpenCodeRuntime removes a launch directory without ever applying a
// pathname chmod. Provider-created directories may be mode 000, so each one is
// restored relative to an already-open parent with AT_SYMLINK_NOFOLLOW and then
// opened with O_NOFOLLOW before recursion. A concurrent directory-to-symlink
// replacement therefore fails closed instead of redirecting chmod or traversal
// outside the runtime.
func cleanupOpenCodeRuntime(parent, root string) error {
	parentFD, err := unix.Open(parent,
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("open runtime parent without following links: %w", err)
	}
	defer func() { _ = unix.Close(parentFD) }()

	base := filepath.Base(root)
	var before unix.Stat_t
	if err := unix.Fstatat(parentFD, base, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("inspect runtime root without following links: %w", err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("runtime root is not a directory")
	}
	rootFD, opened, err := openRuntimeDirectory(parentFD, base, before)
	if err != nil {
		return fmt.Errorf("open runtime root: %w", err)
	}
	if err := removeRuntimeDirectoryContents(rootFD); err != nil {
		return err
	}

	var after unix.Stat_t
	if err := unix.Fstatat(parentFD, base, &after, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("reinspect runtime root without following links: %w", err)
	}
	if after.Mode&unix.S_IFMT != unix.S_IFDIR ||
		after.Dev != opened.Dev || after.Ino != opened.Ino {
		return fmt.Errorf("runtime root changed during cleanup")
	}
	if err := unix.Unlinkat(parentFD, base, unix.AT_REMOVEDIR); err != nil {
		return fmt.Errorf("remove runtime root without following links: %w", err)
	}
	return nil
}

// removeRuntimeDirectoryContents owns fd and closes it before returning.
func removeRuntimeDirectoryContents(fd int) error {
	if fd < 0 {
		return fmt.Errorf("open runtime directory returned invalid descriptor")
	}
	directory := os.NewFile(uintptr(fd), "OpenCode runtime directory")
	if directory == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open runtime directory handle")
	}
	names, readErr := directory.Readdirnames(-1)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		_ = directory.Close()
		return fmt.Errorf("read runtime directory: %w", readErr)
	}
	dirFD := fd
	for _, name := range names {
		if err := removeRuntimeEntry(dirFD, name); err != nil {
			_ = directory.Close()
			return err
		}
	}
	if err := directory.Close(); err != nil {
		return fmt.Errorf("close runtime directory: %w", err)
	}
	return nil
}

func removeRuntimeEntry(parentFD int, name string) error {
	var info unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("inspect runtime entry without following links: %w", err)
	}
	if info.Mode&unix.S_IFMT != unix.S_IFDIR {
		if err := unix.Unlinkat(parentFD, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return fmt.Errorf("remove runtime entry without following links: %w", err)
		}
		return nil
	}
	childFD, opened, err := openRuntimeDirectory(parentFD, name, info)
	if err != nil {
		return fmt.Errorf("open runtime directory: %w", err)
	}
	if err := removeRuntimeDirectoryContents(childFD); err != nil {
		return err
	}
	var after unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &after, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return fmt.Errorf("reinspect runtime directory without following links: %w", err)
	}
	if after.Mode&unix.S_IFMT != unix.S_IFDIR ||
		after.Dev != opened.Dev || after.Ino != opened.Ino {
		return fmt.Errorf("runtime directory changed during cleanup")
	}
	if err := unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("remove runtime directory without following links: %w", err)
	}
	return nil
}

func openRuntimeDirectory(parentFD int, name string, expected unix.Stat_t) (int, unix.Stat_t, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_DIRECTORY
	fd, err := unix.Openat(parentFD, name, flags, 0)
	if err != nil {
		// A provider may deliberately remove traversal. Restore only the exact
		// entry relative to our open parent and only when the kernel can enforce
		// AT_SYMLINK_NOFOLLOW. Older Linux kernels fail closed here rather than
		// falling back to a pathname chmod.
		if chmodErr := unix.Fchmodat(parentFD, name, 0o700, unix.AT_SYMLINK_NOFOLLOW); chmodErr != nil {
			return -1, unix.Stat_t{}, fmt.Errorf("restore traversal without following links: %w", chmodErr)
		}
		fd, err = unix.Openat(parentFD, name, flags, 0)
		if err != nil {
			return -1, unix.Stat_t{}, fmt.Errorf("open without following links: %w", err)
		}
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, fmt.Errorf("inspect opened directory: %w", err)
	}
	if expected.Dev != opened.Dev || expected.Ino != opened.Ino {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, fmt.Errorf("directory changed during cleanup")
	}
	if err := unix.Fchmod(fd, 0o700); err != nil {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, fmt.Errorf("restore traversal on opened directory: %w", err)
	}
	return fd, opened, nil
}
