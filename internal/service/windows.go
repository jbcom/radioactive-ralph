//go:build windows

package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsServiceWaitTimeout    = 30 * time.Second
	windowsServicePollInterval   = 250 * time.Millisecond
	windowsServiceDeleteAttempts = int(windowsServiceWaitTimeout/windowsServicePollInterval) + 1

	windowsServiceInspectAccess         = windows.SERVICE_QUERY_CONFIG
	windowsServiceDeletionInspectAccess = windows.SERVICE_QUERY_STATUS
	windowsServiceStopAccess            = windows.SERVICE_QUERY_CONFIG | windows.SERVICE_QUERY_STATUS | windows.SERVICE_STOP
	windowsServiceUninstallAccess       = windows.SERVICE_QUERY_CONFIG | windows.SERVICE_QUERY_STATUS | windows.SERVICE_STOP | windows.DELETE

	windowsServiceHistoricalDisplayName = "radioactive_ralph supervisor"
	windowsServiceHistoricalDescription = "Durable radioactive_ralph supervisor"
)

var errWindowsSCMLegacyOwnershipUnverified = errors.New(
	"service: native Windows SCM registration is not a recognized legacy radioactive_ralph definition",
)

// windowsSCM and windowsServiceHandle keep the lifecycle testable without
// requiring an elevated test process or mutating the host's real SCM.
type windowsSCM interface {
	OpenService(name string, access uint32) (windowsServiceHandle, error)
	Disconnect() error
}

type windowsServiceHandle interface {
	Close() error
	Config() (mgr.Config, error)
	Query() (svc.Status, error)
	Control(svc.Cmd) (svc.Status, error)
	Delete() error
}

type nativeWindowsSCM struct {
	handle windows.Handle
}

var openWindowsSCManager = windows.OpenSCManager
var openWindowsService = windows.OpenService
var closeWindowsServiceHandle = windows.CloseServiceHandle
var windowsServiceDeletionAttempts = windowsServiceDeleteAttempts
var windowsServiceDeletionSleep = time.Sleep

var connectWindowsSCM = func() (windowsSCM, error) {
	handle, err := openWindowsSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, err
	}
	return &nativeWindowsSCM{handle: handle}, nil
}

func (m *nativeWindowsSCM) OpenService(name string, access uint32) (windowsServiceHandle, error) {
	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	handle, err := openWindowsService(m.handle, namePointer, access)
	if err != nil {
		return nil, err
	}
	return &mgr.Service{Name: name, Handle: handle}, nil
}

func (m *nativeWindowsSCM) Disconnect() error {
	return closeWindowsServiceHandle(m.handle)
}

// inspectWindowsService asks SCM directly rather than trusting the legacy JSON
// config file. This is remediation-only: it lets status find registrations
// created by an earlier development build without loading or starting them.
func inspectWindowsService(opts InstallOptions) (Status, error) {
	status := Status{
		Backend:  BackendWindowsSCM,
		UnitPath: UnitName(BackendWindowsSCM),
	}
	expectedConfigPath, err := expectedLegacyWindowsServiceConfigPath(opts)
	if err != nil {
		return status, err
	}
	manager, err := connectWindowsSCM()
	if err != nil {
		return status, fmt.Errorf("service: connect SCM: %w", err)
	}
	defer func() { _ = manager.Disconnect() }()

	name := UnitName(BackendWindowsSCM)
	s, err := manager.OpenService(name, windowsServiceInspectAccess)
	switch {
	case err == nil:
		if verifyErr := verifyLegacyWindowsServiceOwnership(s, expectedConfigPath); verifyErr != nil {
			status.Installed = true
			_ = s.Close()
			return status, fmt.Errorf("service: verify %s during inspection: %w", name, verifyErr)
		}
		if closeErr := s.Close(); closeErr != nil {
			return status, fmt.Errorf("service: close %s after inspect: %w", name, closeErr)
		}
		status.Installed = true
		return status, nil
	case errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST):
		return status, nil
	case errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE):
		status.Installed = true
		return status, newWindowsSCMDeletionPendingError(name, "inspection")
	default:
		return status, fmt.Errorf("service: open %s: %w", name, err)
	}
}

func stopWindowsService(opts InstallOptions) error {
	expectedConfigPath, err := expectedLegacyWindowsServiceConfigPath(opts)
	if err != nil {
		return err
	}
	manager, err := connectWindowsSCM()
	if err != nil {
		return fmt.Errorf("service: connect SCM: %w", err)
	}
	defer func() { _ = manager.Disconnect() }()

	name := UnitName(BackendWindowsSCM)
	s, err := manager.OpenService(name, windowsServiceStopAccess)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
		return newWindowsSCMDeletionPendingError(name, "stop")
	}
	if err != nil {
		return fmt.Errorf("service: open %s: %w", name, err)
	}
	defer func() { _ = s.Close() }()
	if err := verifyLegacyWindowsServiceOwnership(s, expectedConfigPath); err != nil {
		return fmt.Errorf("service: verify %s before stop: %w", name, err)
	}
	if err := ensureWindowsServiceStopped(s, expectedConfigPath); err != nil {
		return fmt.Errorf("service: stop %s: %w", name, err)
	}
	return nil
}

func uninstallWindowsService(opts InstallOptions, path string) error {
	expectedConfigPath, err := expectedLegacyWindowsServiceConfigPath(opts)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(path), filepath.Clean(expectedConfigPath)) {
		return fmt.Errorf(
			"service: refuse native Windows uninstall with non-canonical config path %q; want %q",
			path,
			expectedConfigPath,
		)
	}
	manager, err := connectWindowsSCM()
	if err != nil {
		return fmt.Errorf("service: connect SCM: %w", err)
	}
	managerConnected := true
	defer func() {
		if managerConnected {
			_ = manager.Disconnect()
		}
	}()

	name := UnitName(BackendWindowsSCM)
	s, err := manager.OpenService(name, windowsServiceUninstallAccess)
	switch {
	case errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST):
		// Already absent is the successful idempotent state.
	case errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE):
		return newWindowsSCMDeletionPendingError(name, "uninstall")
	case err != nil:
		return fmt.Errorf("service: open %s: %w", name, err)
	default:
		serviceOpen := true
		defer func() {
			if serviceOpen {
				_ = s.Close()
			}
		}()
		if err := verifyLegacyWindowsServiceOwnership(s, expectedConfigPath); err != nil {
			return fmt.Errorf("service: verify %s before uninstall: %w", name, err)
		}
		if err := ensureWindowsServiceStopped(s, expectedConfigPath); err != nil {
			return fmt.Errorf("service: stop %s before delete: %w", name, err)
		}
		// Stop can run arbitrary service code and creates a time window in
		// which another administrator could replace ImagePath. Re-query the
		// live SCM definition immediately before Delete; the stable service
		// name alone never authorizes removal.
		if err := verifyLegacyWindowsServiceOwnership(s, expectedConfigPath); err != nil {
			return fmt.Errorf("service: re-verify %s immediately before delete: %w", name, err)
		}
		if err := s.Delete(); err != nil {
			if errors.Is(err, windows.ERROR_SERVICE_MARKED_FOR_DELETE) {
				return newWindowsSCMDeletionPendingError(name, "uninstall")
			}
			return fmt.Errorf("service: delete %s: %w", name, err)
		}
		serviceOpen = false
		if err := s.Close(); err != nil {
			return fmt.Errorf("service: close %s after delete: %w", name, err)
		}
		managerConnected = false
		if err := manager.Disconnect(); err != nil {
			return fmt.Errorf("service: disconnect SCM after deleting %s: %w", name, err)
		}
		if err := waitWindowsServiceDeleted(
			name,
			windowsServiceDeletionAttempts,
			windowsServiceDeletionSleep,
		); err != nil {
			return err
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove %s: %w", path, err)
	}
	return nil
}

func waitWindowsServiceDeleted(
	name string,
	attempts int,
	sleep func(time.Duration),
) error {
	if attempts < 1 {
		attempts = 1
	}
	if sleep == nil {
		sleep = time.Sleep
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		manager, err := connectWindowsSCM()
		if err != nil {
			return fmt.Errorf("service: connect SCM while verifying deletion of %s: %w", name, err)
		}
		s, openErr := manager.OpenService(name, windowsServiceDeletionInspectAccess)
		if openErr == nil {
			closeErr := s.Close()
			disconnectErr := manager.Disconnect()
			if closeErr != nil {
				return fmt.Errorf("service: close %s while verifying deletion: %w", name, closeErr)
			}
			if disconnectErr != nil {
				return fmt.Errorf("service: disconnect SCM while verifying deletion of %s: %w", name, disconnectErr)
			}
			if attempt == attempts {
				return newWindowsSCMDeletionPendingError(name, "uninstall")
			}
			sleep(windowsServicePollInterval)
			continue
		}
		if disconnectErr := manager.Disconnect(); disconnectErr != nil {
			return fmt.Errorf("service: disconnect SCM while verifying deletion of %s: %w", name, disconnectErr)
		}
		switch {
		case errors.Is(openErr, windows.ERROR_SERVICE_DOES_NOT_EXIST):
			return nil
		case errors.Is(openErr, windows.ERROR_SERVICE_MARKED_FOR_DELETE):
			if attempt == attempts {
				return newWindowsSCMDeletionPendingError(name, "uninstall")
			}
			sleep(windowsServicePollInterval)
		default:
			return fmt.Errorf("service: open %s while verifying deletion: %w", name, openErr)
		}
	}
	return newWindowsSCMDeletionPendingError(name, "uninstall")
}

// verifyLegacyWindowsServiceOwnership proves that the stable service name
// still refers to the exact command shape emitted by Ralph's retired native
// SCM installer before Stop or Delete can mutate it. The legacy installer
// composed:
//
//	<absolute radioactive_ralph[.exe]> --supervisor --windows-service-config <home>\AppData\Local\radioactive-ralph\services\radioactive_ralph-supervisor.json
//
// The resolved home may vary, but the config argument must equal UnitPath for
// that exact home. The own-process type, historical display metadata,
// executable basename, exact marker argv, and canonical config path are the
// additive provenance contract. Unknown same-name registrations fail closed
// and retain both SCM state and the historical config file.
func verifyLegacyWindowsServiceOwnership(
	s windowsServiceHandle,
	expectedConfigPath string,
) error {
	config, err := s.Config()
	if err != nil {
		return fmt.Errorf("query SCM configuration: %w", err)
	}
	if config.ServiceType != windows.SERVICE_WIN32_OWN_PROCESS ||
		config.DisplayName != windowsServiceHistoricalDisplayName ||
		config.Description != windowsServiceHistoricalDescription {
		return fmt.Errorf(
			"%w: service type/display name/description do not match the historical definition",
			errWindowsSCMLegacyOwnershipUnverified,
		)
	}
	argv, err := windows.DecomposeCommandLine(config.BinaryPathName)
	if err != nil {
		return fmt.Errorf("%w: cannot parse ImagePath: %v", errWindowsSCMLegacyOwnershipUnverified, err)
	}
	if len(argv) != 4 {
		return fmt.Errorf(
			"%w: want executable plus 3 legacy arguments, got %d command elements",
			errWindowsSCMLegacyOwnershipUnverified,
			len(argv),
		)
	}

	executable := argv[0]
	executableBase := filepath.Base(executable)
	if !filepath.IsAbs(executable) ||
		(!strings.EqualFold(executableBase, "radioactive_ralph.exe") &&
			!strings.EqualFold(executableBase, "radioactive_ralph")) {
		return fmt.Errorf(
			"%w: executable must be an absolute radioactive_ralph(.exe) path",
			errWindowsSCMLegacyOwnershipUnverified,
		)
	}
	if argv[1] != "--supervisor" || argv[2] != "--windows-service-config" {
		return fmt.Errorf(
			"%w: missing exact --supervisor --windows-service-config markers",
			errWindowsSCMLegacyOwnershipUnverified,
		)
	}
	configPath := argv[3]
	if !filepath.IsAbs(configPath) ||
		!strings.EqualFold(filepath.Clean(configPath), filepath.Clean(expectedConfigPath)) {
		return fmt.Errorf(
			"%w: legacy config path must equal canonical UnitPath %q",
			errWindowsSCMLegacyOwnershipUnverified,
			expectedConfigPath,
		)
	}
	return nil
}

func expectedLegacyWindowsServiceConfigPath(opts InstallOptions) (string, error) {
	home := opts.HomeDir
	if home == "" {
		resolved, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("service: user home for legacy Windows SCM ownership: %w", err)
		}
		home = resolved
	}
	expected := filepath.Clean(UnitPath(BackendWindowsSCM, home))
	if !filepath.IsAbs(expected) {
		return "", fmt.Errorf(
			"service: legacy Windows SCM ownership requires an absolute home path, got %q",
			home,
		)
	}
	return expected, nil
}

func newWindowsSCMDeletionPendingError(name, operation string) error {
	return &WindowsSCMDeletionPendingError{
		ServiceName: name,
		Operation:   operation,
	}
}

func ensureWindowsServiceStopped(
	s windowsServiceHandle,
	expectedConfigPath string,
) error {
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
			// A pending transition may have given another administrator time
			// to replace ImagePath. Never rely on the earlier ownership query
			// when crossing the mutating ControlService boundary.
			if err := verifyLegacyWindowsServiceOwnership(s, expectedConfigPath); err != nil {
				return fmt.Errorf("re-verify immediately before stop control: %w", err)
			}
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
