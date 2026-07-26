//go:build !windows

package service

import "fmt"

func inspectWindowsService(_ InstallOptions) (Status, error) {
	return Status{Backend: BackendWindowsSCM}, fmt.Errorf("%w: windows-scm inspection requires Windows", ErrUnsupportedBackend)
}

func uninstallWindowsService(_ InstallOptions, _ string) error {
	return fmt.Errorf("%w: windows-scm", ErrUnsupportedBackend)
}

func stopWindowsService(_ InstallOptions) error {
	return fmt.Errorf("%w: windows-scm", ErrUnsupportedBackend)
}
