//go:build windows

package service

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsServiceWaitTimeout  = 30 * time.Second
	windowsServicePollInterval = 250 * time.Millisecond
)

// windowsSCM and windowsServiceHandle keep the lifecycle testable without
// requiring an elevated test process or mutating the host's real SCM.
type windowsSCM interface {
	OpenService(name string) (windowsServiceHandle, error)
	CreateService(name, exepath string, config mgr.Config, args ...string) (windowsServiceHandle, error)
	Disconnect() error
}

type windowsServiceHandle interface {
	Close() error
	Config() (mgr.Config, error)
	UpdateConfig(mgr.Config) error
	Query() (svc.Status, error)
	Start(args ...string) error
	Control(svc.Cmd) (svc.Status, error)
	Delete() error
}

type nativeWindowsSCM struct {
	manager *mgr.Mgr
}

var connectWindowsSCM = func() (windowsSCM, error) {
	manager, err := mgr.Connect()
	if err != nil {
		return nil, err
	}
	return &nativeWindowsSCM{manager: manager}, nil
}

func (m *nativeWindowsSCM) OpenService(name string) (windowsServiceHandle, error) {
	return m.manager.OpenService(name)
}

func (m *nativeWindowsSCM) CreateService(
	name,
	exepath string,
	config mgr.Config,
	args ...string,
) (windowsServiceHandle, error) {
	return m.manager.CreateService(name, exepath, config, args...)
}

func (m *nativeWindowsSCM) Disconnect() error {
	return m.manager.Disconnect()
}

func installWindowsService(opts InstallOptions, path string) (string, error) {
	raw, err := MarshalWindowsServiceConfig(opts)
	if err != nil {
		return "", fmt.Errorf("service: marshal windows config: %w", err)
	}

	manager, err := connectWindowsSCM()
	if err != nil {
		return "", fmt.Errorf("service: connect SCM: %w", err)
	}
	defer func() { _ = manager.Disconnect() }()

	s, created, err := reconcileWindowsServiceDefinition(manager, opts, path)
	if err != nil {
		return "", err
	}
	defer func() { _ = s.Close() }()

	// Persist the environment only after the SCM definition has reconciled,
	// but before starting it. The explicit config path in BinaryPathName lets
	// the service load the installing user's values even when SCM launches the
	// process under a different account.
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		if created {
			_ = s.Delete()
		}
		return "", fmt.Errorf("service: write %s: %w", path, err)
	}
	if err := startWindowsServiceHandle(s); err != nil {
		return "", fmt.Errorf("service: start %s: %w", UnitName(BackendWindowsSCM), err)
	}
	return path, nil
}

func reconcileWindowsServiceDefinition(
	manager windowsSCM,
	opts InstallOptions,
	configPath string,
) (windowsServiceHandle, bool, error) {
	name := UnitName(BackendWindowsSCM)
	s, err := manager.OpenService(name)
	switch {
	case err == nil:
		if err := ensureWindowsServiceStopped(s); err != nil {
			_ = s.Close()
			return nil, false, fmt.Errorf("service: stop existing %s: %w", name, err)
		}
		config, err := s.Config()
		if err != nil {
			_ = s.Close()
			return nil, false, fmt.Errorf("service: read %s config: %w", name, err)
		}
		config.BinaryPathName = windows.ComposeCommandLine(
			append([]string{opts.RalphBin}, WindowsServiceArgsForConfig(configPath)...),
		)
		config.DisplayName = "radioactive_ralph supervisor"
		config.Description = "Durable radioactive_ralph supervisor"
		config.StartType = mgr.StartAutomatic
		if err := s.UpdateConfig(config); err != nil {
			_ = s.Close()
			return nil, false, fmt.Errorf("service: update %s: %w", name, err)
		}
		return s, false, nil
	case errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST):
		config := mgr.Config{
			DisplayName: "radioactive_ralph supervisor",
			Description: "Durable radioactive_ralph supervisor",
			StartType:   mgr.StartAutomatic,
		}
		s, err := manager.CreateService(
			name,
			opts.RalphBin,
			config,
			WindowsServiceArgsForConfig(configPath)...,
		)
		if err != nil {
			return nil, false, fmt.Errorf("service: create %s: %w", name, err)
		}
		return s, true, nil
	default:
		return nil, false, fmt.Errorf("service: open %s: %w", name, err)
	}
}

// startWindowsService is available to service.Start so a stopped but already
// installed Windows service can be brought back without reinstalling it.
func startWindowsService(_ InstallOptions) error {
	manager, err := connectWindowsSCM()
	if err != nil {
		return fmt.Errorf("service: connect SCM: %w", err)
	}
	defer func() { _ = manager.Disconnect() }()

	name := UnitName(BackendWindowsSCM)
	s, err := manager.OpenService(name)
	if err != nil {
		return fmt.Errorf("service: open %s: %w", name, err)
	}
	defer func() { _ = s.Close() }()
	if err := startWindowsServiceHandle(s); err != nil {
		return fmt.Errorf("service: start %s: %w", name, err)
	}
	return nil
}

func startWindowsServiceHandle(s windowsServiceHandle) error {
	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query before start: %w", err)
	}
	switch status.State {
	case svc.Running:
		return nil
	case svc.StartPending:
		return waitWindowsServiceState(s, svc.Running)
	case svc.Stopped:
		// Ready to start below.
	default:
		if err := ensureWindowsServiceStopped(s); err != nil {
			return err
		}
	}
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return err
	}
	return waitWindowsServiceState(s, svc.Running)
}

func stopWindowsService(_ InstallOptions) error {
	manager, err := connectWindowsSCM()
	if err != nil {
		return fmt.Errorf("service: connect SCM: %w", err)
	}
	defer func() { _ = manager.Disconnect() }()

	name := UnitName(BackendWindowsSCM)
	s, err := manager.OpenService(name)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) ||
		errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("service: open %s: %w", name, err)
	}
	defer func() { _ = s.Close() }()
	if err := ensureWindowsServiceStopped(s); err != nil {
		return fmt.Errorf("service: stop %s: %w", name, err)
	}
	return nil
}

func uninstallWindowsService(_ InstallOptions, path string) error {
	manager, err := connectWindowsSCM()
	if err != nil {
		return fmt.Errorf("service: connect SCM: %w", err)
	}
	defer func() { _ = manager.Disconnect() }()

	name := UnitName(BackendWindowsSCM)
	s, err := manager.OpenService(name)
	switch {
	case errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST),
		errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE):
		// Already absent is the successful idempotent state.
	case err != nil:
		return fmt.Errorf("service: open %s: %w", name, err)
	default:
		if err := ensureWindowsServiceStopped(s); err != nil {
			_ = s.Close()
			return fmt.Errorf("service: stop %s before delete: %w", name, err)
		}
		if err := s.Delete(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
			_ = s.Close()
			return fmt.Errorf("service: delete %s: %w", name, err)
		}
		if err := s.Close(); err != nil {
			return fmt.Errorf("service: close %s after delete: %w", name, err)
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove %s: %w", path, err)
	}
	return nil
}

func ensureWindowsServiceStopped(s windowsServiceHandle) error {
	deadline := time.Now().Add(windowsServiceWaitTimeout)
	for {
		status, err := s.Query()
		if err != nil {
			return fmt.Errorf("query while stopping: %w", err)
		}
		switch status.State {
		case svc.Stopped:
			return nil
		case svc.StopPending:
			// The service has accepted the request; wait below.
		case svc.StartPending, svc.ContinuePending, svc.PausePending:
			// SCM rejects controls while another transition is pending; wait
			// until the service reaches a controllable state.
		default:
			if _, err := s.Control(svc.Stop); err != nil {
				if errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
					return nil
				}
				if !errors.Is(err, windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL) {
					return fmt.Errorf("send stop control: %w", err)
				}
			}
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("timeout waiting for Stopped (last state %d)", status.State)
		}
		time.Sleep(windowsServicePollInterval)
	}
}

func waitWindowsServiceState(s windowsServiceHandle, target svc.State) error {
	deadline := time.Now().Add(windowsServiceWaitTimeout)
	var last svc.State
	for {
		status, err := s.Query()
		if err != nil {
			return fmt.Errorf("query while waiting for state %d: %w", target, err)
		}
		last = status.State
		if last == target {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("timeout waiting for state %d (last state %d)", target, last)
		}
		time.Sleep(windowsServicePollInterval)
	}
}
