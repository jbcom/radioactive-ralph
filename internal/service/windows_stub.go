//go:build !windows

package service

import "fmt"

func installWindowsService(_ InstallOptions, _ string) (string, error) {
	return "", fmt.Errorf("%w: windows-scm", ErrUnsupportedBackend)
}

func uninstallWindowsService(_ InstallOptions, _ string) error {
	return fmt.Errorf("%w: windows-scm", ErrUnsupportedBackend)
}
