//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !windows

package main

func servicePathDirAllowed(string) bool {
	return false
}
