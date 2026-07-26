//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

func servicePathDirAllowed(candidate string) bool {
	effectiveUID := uint32(os.Geteuid()) //nolint:gosec // Unix uid_t is uint32
	if unixInferredServicePathAllowed(
		candidate,
		effectiveUID,
		readServicePathMetadata,
	) {
		return true
	}
	return platformInferredServicePathAllowed(candidate, effectiveUID)
}

func unixInferredServicePathAllowed(
	candidate string,
	effectiveUID uint32,
	metadata func(string) (servicePathMetadata, bool),
) bool {
	clean := filepath.Clean(candidate)
	if !filepath.IsAbs(clean) {
		return false
	}

	components := []string{clean}
	for current := clean; ; {
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		components = append(components, parent)
		current = parent
	}
	for index := len(components) - 1; index >= 0; index-- {
		value, ok := metadata(components[index])
		if !ok || !genericInferredServicePathAllowed(value, effectiveUID) {
			return false
		}
	}
	return true
}

func readServicePathMetadata(candidate string) (servicePathMetadata, bool) {
	info, err := os.Lstat(candidate)
	if err != nil {
		return servicePathMetadata{}, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return servicePathMetadata{}, false
	}
	return servicePathMetadata{
		mode:      info.Mode(),
		uid:       stat.Uid,
		gid:       stat.Gid,
		directory: info.IsDir(),
		symlink:   info.Mode()&os.ModeSymlink != 0,
	}, true
}
