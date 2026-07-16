//go:build windows

package service

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/svc/mgr"
)

func installWindowsService(opts InstallOptions, path string) (string, error) {
	raw, err := MarshalWindowsServiceConfig(opts)
	if err != nil {
		return "", fmt.Errorf("service: marshal windows config: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", fmt.Errorf("service: write %s: %w", path, err)
	}

	manager, err := mgr.Connect()
	if err != nil {
		return "", fmt.Errorf("service: connect SCM: %w", err)
	}
	defer manager.Disconnect()

	name := UnitName(BackendWindowsSCM)
	s, err := manager.CreateService(name, opts.RalphBin, mgr.Config{
		DisplayName: "radioactive_ralph supervisor",
		StartType:   mgr.StartAutomatic,
	}, WindowsServiceArgs()...)
	if err != nil {
		return "", fmt.Errorf("service: create %s: %w", name, err)
	}
	defer s.Close()
	return path, nil
}

func uninstallWindowsService(_ InstallOptions, path string) error {
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("service: connect SCM: %w", err)
	}
	defer manager.Disconnect()

	name := UnitName(BackendWindowsSCM)
	s, err := manager.OpenService(name)
	if err == nil {
		defer s.Close()
		if err := s.Delete(); err != nil {
			return fmt.Errorf("service: delete %s: %w", name, err)
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove %s: %w", path, err)
	}
	return nil
}
