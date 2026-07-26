//go:build windows

package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type fakeWindowsSCM struct {
	service         *fakeWindowsService
	openErr         error
	disconnectErr   error
	openCalls       int
	disconnectCalls int
	openNames       []string
	openAccesses    []uint32
}

func (f *fakeWindowsSCM) OpenService(name string, access uint32) (windowsServiceHandle, error) {
	f.openCalls++
	f.openNames = append(f.openNames, name)
	f.openAccesses = append(f.openAccesses, access)
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.service, nil
}

func (f *fakeWindowsSCM) Disconnect() error {
	f.disconnectCalls++
	return f.disconnectErr
}

type fakeWindowsService struct {
	states       []svc.State
	queryIndex   int
	queryCalls   int
	config       mgr.Config
	configSet    bool
	configs      []mgr.Config
	configIndex  int
	configErr    error
	configCalls  int
	controlCalls []svc.Cmd
	deleteCalls  int
	closeCalls   int
	deleteErr    error
	closeErr     error
}

func (f *fakeWindowsService) Close() error {
	f.closeCalls++
	return f.closeErr
}

func (f *fakeWindowsService) Config() (mgr.Config, error) {
	f.configCalls++
	if f.configErr != nil {
		return mgr.Config{}, f.configErr
	}
	if len(f.configs) != 0 {
		index := f.configIndex
		if index >= len(f.configs) {
			index = len(f.configs) - 1
		}
		if f.configIndex < len(f.configs)-1 {
			f.configIndex++
		}
		return f.configs[index], nil
	}
	if !f.configSet && f.config.BinaryPathName == "" {
		return recognizedLegacyWindowsServiceConfig(), nil
	}
	return f.config, nil
}

func (f *fakeWindowsService) Query() (svc.Status, error) {
	f.queryCalls++
	if len(f.states) == 0 {
		return svc.Status{}, errors.New("no fake state")
	}
	index := f.queryIndex
	if index >= len(f.states) {
		index = len(f.states) - 1
	}
	if f.queryIndex < len(f.states)-1 {
		f.queryIndex++
	}
	return svc.Status{State: f.states[index]}, nil
}

func (f *fakeWindowsService) Control(cmd svc.Cmd) (svc.Status, error) {
	f.controlCalls = append(f.controlCalls, cmd)
	return svc.Status{State: svc.StopPending}, nil
}

func (f *fakeWindowsService) Delete() error {
	f.deleteCalls++
	return f.deleteErr
}

func TestConnectWindowsSCMRequestsConnectAccessOnly(t *testing.T) {
	const managerHandle windows.Handle = 41
	var gotAccess uint32
	var closed windows.Handle

	originalOpenManager := openWindowsSCManager
	originalCloseHandle := closeWindowsServiceHandle
	openWindowsSCManager = func(_, _ *uint16, access uint32) (windows.Handle, error) {
		gotAccess = access
		return managerHandle, nil
	}
	closeWindowsServiceHandle = func(handle windows.Handle) error {
		closed = handle
		return nil
	}
	t.Cleanup(func() {
		openWindowsSCManager = originalOpenManager
		closeWindowsServiceHandle = originalCloseHandle
	})

	manager, err := connectWindowsSCM()
	if err != nil {
		t.Fatalf("connectWindowsSCM: %v", err)
	}
	if gotAccess != windows.SC_MANAGER_CONNECT {
		t.Fatalf("OpenSCManager access = %#x, want SC_MANAGER_CONNECT (%#x)", gotAccess, windows.SC_MANAGER_CONNECT)
	}
	if err := manager.Disconnect(); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if closed != managerHandle {
		t.Fatalf("closed SCM handle = %d, want %d", closed, managerHandle)
	}
}

func TestNativeWindowsSCMForwardsRequestedServiceAccess(t *testing.T) {
	const (
		managerHandle windows.Handle = 41
		serviceHandle windows.Handle = 42
		wantAccess                   = windows.SERVICE_QUERY_STATUS | windows.SERVICE_STOP
	)
	var gotManager windows.Handle
	var gotName string
	var gotAccess uint32

	originalOpenService := openWindowsService
	openWindowsService = func(manager windows.Handle, name *uint16, access uint32) (windows.Handle, error) {
		gotManager = manager
		gotName = windows.UTF16PtrToString(name)
		gotAccess = access
		return serviceHandle, nil
	}
	t.Cleanup(func() { openWindowsService = originalOpenService })

	handle, err := (&nativeWindowsSCM{handle: managerHandle}).OpenService("ralph-test", wantAccess)
	if err != nil {
		t.Fatalf("OpenService: %v", err)
	}
	native, ok := handle.(*mgr.Service)
	if !ok {
		t.Fatalf("service handle type = %T, want *mgr.Service", handle)
	}
	if gotManager != managerHandle || gotName != "ralph-test" || gotAccess != wantAccess {
		t.Fatalf(
			"OpenService call = (manager=%d, name=%q, access=%#x), want (%d, %q, %#x)",
			gotManager,
			gotName,
			gotAccess,
			managerHandle,
			"ralph-test",
			wantAccess,
		)
	}
	if native.Name != "ralph-test" || native.Handle != serviceHandle {
		t.Fatalf("native service = %+v, want name ralph-test handle %d", native, serviceHandle)
	}
}

func TestWindowsServiceRemediationAccessIsLeastPrivilege(t *testing.T) {
	if windowsServiceInspectAccess != windows.SERVICE_QUERY_CONFIG {
		t.Fatalf(
			"inspect access = %#x, want SERVICE_QUERY_CONFIG (%#x)",
			windowsServiceInspectAccess,
			windows.SERVICE_QUERY_CONFIG,
		)
	}
	if windowsServiceDeletionInspectAccess != windows.SERVICE_QUERY_STATUS {
		t.Fatalf(
			"deletion inspection access = %#x, want SERVICE_QUERY_STATUS (%#x)",
			windowsServiceDeletionInspectAccess,
			windows.SERVICE_QUERY_STATUS,
		)
	}
	wantStop := uint32(windows.SERVICE_QUERY_CONFIG | windows.SERVICE_QUERY_STATUS | windows.SERVICE_STOP)
	if windowsServiceStopAccess != wantStop {
		t.Fatalf("stop access = %#x, want %#x", windowsServiceStopAccess, wantStop)
	}
	wantUninstall := uint32(
		windows.SERVICE_QUERY_CONFIG |
			windows.SERVICE_QUERY_STATUS |
			windows.SERVICE_STOP |
			windows.DELETE,
	)
	if windowsServiceUninstallAccess != wantUninstall {
		t.Fatalf("uninstall access = %#x, want %#x", windowsServiceUninstallAccess, wantUninstall)
	}
}

func TestWindowsSCMInstallAndStartRejectBeforeMutation(t *testing.T) {
	connectCalls := 0
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) {
		connectCalls++
		return nil, errors.New("must not connect")
	}
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	home := filepath.Join(t.TempDir(), "must-not-exist")
	_, installErr := Install(InstallOptions{
		Backend: BackendWindowsSCM,
		HomeDir: home,
		// Deliberately omit RalphBin and use invalid environment data. The
		// backend rejection is the first operation and cannot create a path or
		// consult SCM while trying to validate an unsafe definition.
		ExtraEnv: map[string]string{"NOT VALID": "value\n"},
	})
	assertWindowsSCMDisabled(t, installErr, WindowsSCMOperationInstall)

	startErr := Start(InstallOptions{Backend: BackendWindowsSCM, HomeDir: home})
	assertWindowsSCMDisabled(t, startErr, WindowsSCMOperationStart)

	if connectCalls != 0 {
		t.Fatalf("disabled install/start connected to SCM %d times", connectCalls)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("disabled install/start mutated filesystem at %s: %v", home, err)
	}
}

func TestInspectWindowsServiceFindsLegacyRegistrationWithoutStartingIt(t *testing.T) {
	handle := &fakeWindowsService{}
	manager := &fakeWindowsSCM{service: handle}
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	status, err := Inspect(InstallOptions{Backend: BackendWindowsSCM})
	if err != nil {
		t.Fatalf("Inspect(windows-scm): %v", err)
	}
	if !status.Installed {
		t.Fatal("legacy SCM registration was not reported installed")
	}
	if status.Backend != BackendWindowsSCM {
		t.Fatalf("Backend = %q, want %q", status.Backend, BackendWindowsSCM)
	}
	if status.UnitPath != UnitName(BackendWindowsSCM) {
		t.Fatalf("UnitPath = %q, want SCM service name %q", status.UnitPath, UnitName(BackendWindowsSCM))
	}
	if manager.openCalls != 1 || handle.closeCalls != 1 {
		t.Fatalf("inspect lifecycle: open=%d close=%d, want 1/1", manager.openCalls, handle.closeCalls)
	}
	assertWindowsServiceOpen(t, manager, windowsServiceInspectAccess)
	if handle.configCalls != 1 || handle.queryCalls != 0 ||
		len(handle.controlCalls) != 0 || handle.deleteCalls != 0 {
		t.Fatalf("inspect mutated legacy service: %+v", handle)
	}
}

func TestInspectWindowsServiceReportsLegacyRegistrationAbsent(t *testing.T) {
	manager := &fakeWindowsSCM{openErr: windows.ERROR_SERVICE_DOES_NOT_EXIST}
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	status, err := Inspect(InstallOptions{Backend: BackendWindowsSCM})
	if err != nil {
		t.Fatalf("Inspect(absent windows-scm): %v", err)
	}
	if status.Installed {
		t.Fatal("absent legacy SCM registration reported installed")
	}
	assertWindowsServiceOpen(t, manager, windowsServiceInspectAccess)
}

func TestInspectWindowsServiceRejectsUnknownSameNameRegistration(t *testing.T) {
	handle := &fakeWindowsService{
		config: mgr.Config{BinaryPathName: `C:\Windows\System32\cmd.exe /d /c exit 0`},
	}
	manager := &fakeWindowsSCM{service: handle}
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	status, err := Inspect(InstallOptions{Backend: BackendWindowsSCM})
	if !errors.Is(err, errWindowsSCMLegacyOwnershipUnverified) {
		t.Fatalf("Inspect error = %v, want errWindowsSCMLegacyOwnershipUnverified", err)
	}
	if !status.Installed {
		t.Fatal("unknown same-name registration did not report occupied SCM namespace")
	}
	assertWindowsServiceOpen(t, manager, windowsServiceInspectAccess)
	if handle.configCalls != 1 || handle.queryCalls != 0 ||
		len(handle.controlCalls) != 0 || handle.deleteCalls != 0 {
		t.Fatalf("inspection mutated unknown registration: %+v", handle)
	}
	if handle.closeCalls != 1 || manager.disconnectCalls != 1 {
		t.Fatalf(
			"unknown inspection close/disconnect = %d/%d, want 1/1",
			handle.closeCalls,
			manager.disconnectCalls,
		)
	}
}

func TestStopWindowsServiceUsesMinimalAccessAndWaitsForStopped(t *testing.T) {
	handle := &fakeWindowsService{
		states: []svc.State{svc.Running, svc.StopPending, svc.Stopped},
	}
	manager := &fakeWindowsSCM{service: handle}
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	if err := stopWindowsService(InstallOptions{}); err != nil {
		t.Fatalf("stopWindowsService: %v", err)
	}
	assertWindowsServiceOpen(t, manager, windowsServiceStopAccess)
	if len(handle.controlCalls) != 1 || handle.controlCalls[0] != svc.Stop {
		t.Fatalf("stop controls = %v, want [Stop]", handle.controlCalls)
	}
	if handle.configCalls != 2 {
		t.Fatalf("config calls = %d, want initial and immediate pre-control ownership queries", handle.configCalls)
	}
	if handle.closeCalls != 1 {
		t.Fatalf("close calls = %d, want 1", handle.closeCalls)
	}
}

func TestStopWindowsServiceAbsentIsIdempotent(t *testing.T) {
	manager := &fakeWindowsSCM{openErr: windows.ERROR_SERVICE_DOES_NOT_EXIST}
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	if err := stopWindowsService(InstallOptions{}); err != nil {
		t.Fatalf("stopWindowsService(absent): %v", err)
	}
	assertWindowsServiceOpen(t, manager, windowsServiceStopAccess)
}

func TestStopWindowsServiceRefusesImagePathChangeBeforeControl(t *testing.T) {
	recognized := recognizedLegacyWindowsServiceConfig()
	changed := recognized
	changed.BinaryPathName = `C:\Windows\System32\cmd.exe /d /c exit 0`
	handle := &fakeWindowsService{
		states:  []svc.State{svc.Running},
		configs: []mgr.Config{recognized, changed},
	}
	manager := &fakeWindowsSCM{service: handle}
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	err := stopWindowsService(InstallOptions{})
	if !errors.Is(err, errWindowsSCMLegacyOwnershipUnverified) {
		t.Fatalf("error = %v, want errWindowsSCMLegacyOwnershipUnverified", err)
	}
	if handle.configCalls != 2 || handle.queryCalls != 1 {
		t.Fatalf(
			"ownership/query calls = %d/%d, want 2/1 before Control",
			handle.configCalls,
			handle.queryCalls,
		)
	}
	if len(handle.controlCalls) != 0 || handle.deleteCalls != 0 {
		t.Fatalf("changed ImagePath crossed Control/Delete boundary: %+v", handle)
	}
}

func TestLegacyWindowsServiceOwnershipRecognizesHistoricalImagePath(t *testing.T) {
	tests := []struct {
		name         string
		command      string
		expectedPath string
	}{
		{
			name: "quoted executable and profile paths",
			command: windows.ComposeCommandLine([]string{
				`C:\Program Files\radioactive-ralph\radioactive_ralph.exe`,
				"--supervisor",
				"--windows-service-config",
				`C:\Users\Alice Example\AppData\Local\radioactive-ralph\services\radioactive_ralph-supervisor.json`,
			}),
			expectedPath: `C:\Users\Alice Example\AppData\Local\radioactive-ralph\services\radioactive_ralph-supervisor.json`,
		},
		{
			name: "arbitrary absolute home and binary without exe suffix",
			command: windows.ComposeCommandLine([]string{
				`D:\tools\radioactive_ralph`,
				"--supervisor",
				"--windows-service-config",
				`D:\portable profile\AppData\Local\radioactive-ralph\services\radioactive_ralph-supervisor.json`,
			}),
			expectedPath: `D:\portable profile\AppData\Local\radioactive-ralph\services\radioactive_ralph-supervisor.json`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := recognizedLegacyWindowsServiceConfigForPath(tt.expectedPath)
			config.BinaryPathName = tt.command
			handle := &fakeWindowsService{config: config, configSet: true}
			if err := verifyLegacyWindowsServiceOwnership(handle, tt.expectedPath); err != nil {
				t.Fatalf("verifyLegacyWindowsServiceOwnership: %v", err)
			}
			if handle.configCalls != 1 {
				t.Fatalf("Config calls = %d, want 1", handle.configCalls)
			}
		})
	}
}

func TestLegacyWindowsServiceOwnershipRejectsUnknownImagePaths(t *testing.T) {
	legacyExe := `C:\Program Files\radioactive-ralph\radioactive_ralph.exe`
	legacyConfig := `C:\Users\alice\AppData\Local\radioactive-ralph\services\radioactive_ralph-supervisor.json`
	tests := []struct {
		name    string
		command string
	}{
		{name: "empty", command: ""},
		{
			name: "same-name cmd collision",
			command: windows.ComposeCommandLine([]string{
				`C:\Windows\System32\cmd.exe`,
				"/d",
				"/c",
				"exit 0",
			}),
		},
		{
			name: "relative executable",
			command: windows.ComposeCommandLine([]string{
				"radioactive_ralph.exe",
				"--supervisor",
				"--windows-service-config",
				legacyConfig,
			}),
		},
		{
			name: "lookalike executable",
			command: windows.ComposeCommandLine([]string{
				`C:\tools\radioactive_ralph-helper.exe`,
				"--supervisor",
				"--windows-service-config",
				legacyConfig,
			}),
		},
		{
			name: "missing supervisor marker",
			command: windows.ComposeCommandLine([]string{
				legacyExe,
				"--windows-service-config",
				legacyConfig,
			}),
		},
		{
			name: "swapped markers",
			command: windows.ComposeCommandLine([]string{
				legacyExe,
				"--windows-service-config",
				"--supervisor",
				legacyConfig,
			}),
		},
		{
			name: "extra argument",
			command: windows.ComposeCommandLine([]string{
				legacyExe,
				"--supervisor",
				"--windows-service-config",
				legacyConfig,
				"--version",
			}),
		},
		{
			name: "relative config",
			command: windows.ComposeCommandLine([]string{
				legacyExe,
				"--supervisor",
				"--windows-service-config",
				`radioactive_ralph-supervisor.json`,
			}),
		},
		{
			name: "basename-only portable config",
			command: windows.ComposeCommandLine([]string{
				legacyExe,
				"--supervisor",
				"--windows-service-config",
				`D:\portable profile\radioactive_ralph-supervisor.json`,
			}),
		},
		{
			name: "lookalike suffix missing Local",
			command: windows.ComposeCommandLine([]string{
				legacyExe,
				"--supervisor",
				"--windows-service-config",
				`C:\Users\alice\AppData\radioactive-ralph\services\radioactive_ralph-supervisor.json`,
			}),
		},
		{
			name: "valid suffix under different home",
			command: windows.ComposeCommandLine([]string{
				legacyExe,
				"--supervisor",
				"--windows-service-config",
				`C:\Users\bob\AppData\Local\radioactive-ralph\services\radioactive_ralph-supervisor.json`,
			}),
		},
		{
			name: "suffix directly under volume root has no home",
			command: windows.ComposeCommandLine([]string{
				legacyExe,
				"--supervisor",
				"--windows-service-config",
				`C:\AppData\Local\radioactive-ralph\services\radioactive_ralph-supervisor.json`,
			}),
		},
		{
			name: "wrong config basename",
			command: windows.ComposeCommandLine([]string{
				legacyExe,
				"--supervisor",
				"--windows-service-config",
				`C:\Users\alice\AppData\Local\radioactive-ralph\services\other.json`,
			}),
		},
		{
			name:    "NUL cannot parse",
			command: legacyExe + "\x00 --supervisor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := recognizedLegacyWindowsServiceConfigForPath(legacyConfig)
			config.BinaryPathName = tt.command
			handle := &fakeWindowsService{config: config, configSet: true}
			err := verifyLegacyWindowsServiceOwnership(handle, legacyConfig)
			if !errors.Is(err, errWindowsSCMLegacyOwnershipUnverified) {
				t.Fatalf("error = %v, want errWindowsSCMLegacyOwnershipUnverified", err)
			}
			if handle.configCalls != 1 {
				t.Fatalf("Config calls = %d, want 1", handle.configCalls)
			}
		})
	}
}

func TestLegacyWindowsServiceOwnershipRejectsMetadataMismatch(t *testing.T) {
	expectedPath := `C:\Users\alice\AppData\Local\radioactive-ralph\services\radioactive_ralph-supervisor.json`
	tests := []struct {
		name   string
		mutate func(*mgr.Config)
	}{
		{
			name: "shared process",
			mutate: func(config *mgr.Config) {
				config.ServiceType = windows.SERVICE_WIN32_SHARE_PROCESS
			},
		},
		{
			name: "wrong display name",
			mutate: func(config *mgr.Config) {
				config.DisplayName = "unrelated service"
			},
		},
		{
			name: "wrong description",
			mutate: func(config *mgr.Config) {
				config.Description = "unrelated description"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := recognizedLegacyWindowsServiceConfigForPath(expectedPath)
			tt.mutate(&config)
			handle := &fakeWindowsService{config: config, configSet: true}
			err := verifyLegacyWindowsServiceOwnership(handle, expectedPath)
			if !errors.Is(err, errWindowsSCMLegacyOwnershipUnverified) {
				t.Fatalf("error = %v, want errWindowsSCMLegacyOwnershipUnverified", err)
			}
		})
	}
}

func TestLegacyWindowsServiceOwnershipAllowsOperatorDisabledStartType(t *testing.T) {
	expectedPath := `C:\Users\alice\AppData\Local\radioactive-ralph\services\radioactive_ralph-supervisor.json`
	config := recognizedLegacyWindowsServiceConfigForPath(expectedPath)
	config.StartType = mgr.StartDisabled
	handle := &fakeWindowsService{config: config, configSet: true}

	if err := verifyLegacyWindowsServiceOwnership(handle, expectedPath); err != nil {
		t.Fatalf("disabled recognizable legacy service rejected: %v", err)
	}
}

func TestStopWindowsServiceRefusesUnknownNameCollisionBeforeMutation(t *testing.T) {
	handle := &fakeWindowsService{
		config: mgr.Config{BinaryPathName: `C:\Windows\System32\cmd.exe /d /c exit 0`},
		states: []svc.State{svc.Running},
	}
	manager := &fakeWindowsSCM{service: handle}
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	err := stopWindowsService(InstallOptions{})
	if !errors.Is(err, errWindowsSCMLegacyOwnershipUnverified) {
		t.Fatalf("error = %v, want errWindowsSCMLegacyOwnershipUnverified", err)
	}
	assertWindowsServiceOpen(t, manager, windowsServiceStopAccess)
	if handle.configCalls != 1 || handle.queryCalls != 0 ||
		len(handle.controlCalls) != 0 || handle.deleteCalls != 0 {
		t.Fatalf("unknown collision was mutated: %+v", handle)
	}
	if handle.closeCalls != 1 || manager.disconnectCalls != 1 {
		t.Fatalf(
			"collision cleanup close/disconnect = %d/%d, want 1/1",
			handle.closeCalls,
			manager.disconnectCalls,
		)
	}
}

func TestUninstallWindowsServiceRefusesUnknownNameCollisionAndKeepsConfig(t *testing.T) {
	handle := &fakeWindowsService{
		config: mgr.Config{BinaryPathName: `C:\Windows\System32\cmd.exe /d /c exit 0`},
		states: []svc.State{svc.Stopped},
	}
	manager := &fakeWindowsSCM{service: handle}
	stubWindowsSCMConnections(t, manager)

	opts, path := writeLegacyWindowsServiceConfig(t, "must remain")
	err := uninstallWindowsService(opts, path)
	if !errors.Is(err, errWindowsSCMLegacyOwnershipUnverified) {
		t.Fatalf("error = %v, want errWindowsSCMLegacyOwnershipUnverified", err)
	}

	assertWindowsServiceOpen(t, manager, windowsServiceUninstallAccess)
	if handle.configCalls != 1 || handle.queryCalls != 0 ||
		len(handle.controlCalls) != 0 || handle.deleteCalls != 0 {
		t.Fatalf("unknown collision was mutated: %+v", handle)
	}
	if handle.closeCalls != 1 || manager.disconnectCalls != 1 {
		t.Fatalf(
			"collision cleanup close/disconnect = %d/%d, want 1/1",
			handle.closeCalls,
			manager.disconnectCalls,
		)
	}
	assertWindowsServiceConfig(t, path, "must remain")
}

func TestUninstallWindowsServiceConfigQueryFailureKeepsConfig(t *testing.T) {
	configErr := errors.New("SCM query config denied")
	handle := &fakeWindowsService{
		configErr: configErr,
		states:    []svc.State{svc.Stopped},
	}
	manager := &fakeWindowsSCM{service: handle}
	stubWindowsSCMConnections(t, manager)

	opts, path := writeLegacyWindowsServiceConfig(t, "must remain")
	err := uninstallWindowsService(opts, path)
	if !errors.Is(err, configErr) {
		t.Fatalf("error = %v, want wrapped %v", err, configErr)
	}
	if handle.queryCalls != 0 || len(handle.controlCalls) != 0 || handle.deleteCalls != 0 {
		t.Fatalf("config-query failure mutated service: %+v", handle)
	}
	assertWindowsServiceConfig(t, path, "must remain")
}

func TestUninstallWindowsServiceRejectsNonCanonicalRemovalPathBeforeSCM(t *testing.T) {
	opts, canonicalPath := writeLegacyWindowsServiceConfig(t, "must remain")
	connectCalls := 0
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) {
		connectCalls++
		return nil, errors.New("must not connect")
	}
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	otherPath := filepath.Join(opts.HomeDir, "other-service.json")
	err := uninstallWindowsService(opts, otherPath)
	if err == nil || !strings.Contains(err.Error(), "non-canonical config path") {
		t.Fatalf("error = %v, want non-canonical config path refusal", err)
	}
	if connectCalls != 0 {
		t.Fatalf("non-canonical uninstall connected to SCM %d times", connectCalls)
	}
	assertWindowsServiceConfig(t, canonicalPath, "must remain")
}

func TestUninstallWindowsServiceStopsWaitsDeletesAndIsIdempotent(t *testing.T) {
	handle := &fakeWindowsService{
		states: []svc.State{svc.Running, svc.StopPending, svc.Stopped},
	}
	opts, path := writeLegacyWindowsServiceConfig(t, "{}")
	bindRecognizedLegacyWindowsServiceConfig(handle, path)
	deleteManager := &fakeWindowsSCM{service: handle}
	absenceManager := &fakeWindowsSCM{openErr: windows.ERROR_SERVICE_DOES_NOT_EXIST}
	idempotentManager := &fakeWindowsSCM{openErr: windows.ERROR_SERVICE_DOES_NOT_EXIST}
	stubWindowsSCMConnections(t, deleteManager, absenceManager, idempotentManager)
	sleepCalls := stubWindowsServiceDeletionRetry(t, 3)

	if err := uninstallWindowsService(opts, path); err != nil {
		t.Fatalf("uninstallWindowsService: %v", err)
	}
	if len(handle.controlCalls) != 1 || handle.controlCalls[0] != svc.Stop {
		t.Fatalf("stop controls = %v, want [Stop]", handle.controlCalls)
	}
	if handle.configCalls != 3 {
		t.Fatalf("config calls = %d, want initial, pre-control, and pre-delete ownership queries", handle.configCalls)
	}
	assertWindowsServiceOpen(t, deleteManager, windowsServiceUninstallAccess)
	assertWindowsServiceOpen(t, absenceManager, windowsServiceDeletionInspectAccess)
	if handle.deleteCalls != 1 || handle.closeCalls != 1 {
		t.Fatalf("delete/close calls = %d/%d, want 1/1", handle.deleteCalls, handle.closeCalls)
	}
	if deleteManager.disconnectCalls != 1 || absenceManager.disconnectCalls != 1 {
		t.Fatalf(
			"delete/verification disconnect calls = %d/%d, want 1/1",
			deleteManager.disconnectCalls,
			absenceManager.disconnectCalls,
		)
	}
	if *sleepCalls != 0 {
		t.Fatalf("deletion verification sleeps = %d, want 0", *sleepCalls)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy config remains after uninstall: %v", err)
	}

	if err := uninstallWindowsService(opts, path); err != nil {
		t.Fatalf("second uninstall should be idempotent: %v", err)
	}
	assertWindowsServiceOpen(t, idempotentManager, windowsServiceUninstallAccess)
	if idempotentManager.disconnectCalls != 1 {
		t.Fatalf("idempotent disconnect calls = %d, want 1", idempotentManager.disconnectCalls)
	}
}

func TestUninstallWindowsServiceRefusesImagePathChangeBeforeDeleteAndKeepsConfig(t *testing.T) {
	opts, path := writeLegacyWindowsServiceConfig(t, "must remain")
	recognized := recognizedLegacyWindowsServiceConfigForPath(path)
	changed := recognized
	changed.BinaryPathName = `C:\Windows\System32\cmd.exe /d /c exit 0`
	handle := &fakeWindowsService{
		states:  []svc.State{svc.Running, svc.StopPending, svc.Stopped},
		configs: []mgr.Config{recognized, recognized, changed},
	}
	manager := &fakeWindowsSCM{service: handle}
	stubWindowsSCMConnections(t, manager)

	err := uninstallWindowsService(opts, path)
	if !errors.Is(err, errWindowsSCMLegacyOwnershipUnverified) {
		t.Fatalf("error = %v, want errWindowsSCMLegacyOwnershipUnverified", err)
	}

	assertWindowsServiceOpen(t, manager, windowsServiceUninstallAccess)
	if handle.configCalls != 3 {
		t.Fatalf("config calls = %d, want initial, pre-control, and pre-delete ownership queries", handle.configCalls)
	}
	if len(handle.controlCalls) != 1 || handle.controlCalls[0] != svc.Stop {
		t.Fatalf("stop controls = %v, want [Stop]", handle.controlCalls)
	}
	if handle.deleteCalls != 0 {
		t.Fatalf("changed ImagePath was deleted %d times", handle.deleteCalls)
	}
	if handle.closeCalls != 1 || manager.disconnectCalls != 1 {
		t.Fatalf(
			"TOCTOU refusal close/disconnect = %d/%d, want 1/1",
			handle.closeCalls,
			manager.disconnectCalls,
		)
	}
	assertWindowsServiceConfig(t, path, "must remain")
}

func TestUninstallWindowsServiceWaitsForMarkedDeletionThenRemovesConfig(t *testing.T) {
	handle := &fakeWindowsService{states: []svc.State{svc.Stopped}}
	opts, path := writeLegacyWindowsServiceConfig(t, "remove after proof")
	bindRecognizedLegacyWindowsServiceConfig(handle, path)
	deleteManager := &fakeWindowsSCM{service: handle}
	pendingManager := &fakeWindowsSCM{openErr: windows.ERROR_SERVICE_MARKED_FOR_DELETE}
	absenceManager := &fakeWindowsSCM{openErr: windows.ERROR_SERVICE_DOES_NOT_EXIST}
	stubWindowsSCMConnections(t, deleteManager, pendingManager, absenceManager)
	sleepCalls := stubWindowsServiceDeletionRetry(t, 3)

	if err := uninstallWindowsService(opts, path); err != nil {
		t.Fatalf("uninstallWindowsService: %v", err)
	}

	assertWindowsServiceOpen(t, deleteManager, windowsServiceUninstallAccess)
	assertWindowsServiceOpen(t, pendingManager, windowsServiceDeletionInspectAccess)
	assertWindowsServiceOpen(t, absenceManager, windowsServiceDeletionInspectAccess)
	for index, manager := range []*fakeWindowsSCM{deleteManager, pendingManager, absenceManager} {
		if manager.disconnectCalls != 1 {
			t.Fatalf("manager %d disconnect calls = %d, want 1", index, manager.disconnectCalls)
		}
	}
	if handle.deleteCalls != 1 || handle.closeCalls != 1 {
		t.Fatalf("delete/close calls = %d/%d, want 1/1", handle.deleteCalls, handle.closeCalls)
	}
	if *sleepCalls != 1 {
		t.Fatalf("deletion verification sleeps = %d, want 1", *sleepCalls)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy config remains after proven deletion: %v", err)
	}
}

func TestUninstallWindowsServiceWaitsForPresentRegistrationThenRemovesConfig(t *testing.T) {
	handle := &fakeWindowsService{states: []svc.State{svc.Stopped}}
	opts, path := writeLegacyWindowsServiceConfig(t, "remove after proof")
	bindRecognizedLegacyWindowsServiceConfig(handle, path)
	deleteManager := &fakeWindowsSCM{service: handle}
	presentHandle := &fakeWindowsService{}
	presentManager := &fakeWindowsSCM{service: presentHandle}
	absenceManager := &fakeWindowsSCM{openErr: windows.ERROR_SERVICE_DOES_NOT_EXIST}
	stubWindowsSCMConnections(t, deleteManager, presentManager, absenceManager)
	sleepCalls := stubWindowsServiceDeletionRetry(t, 3)

	if err := uninstallWindowsService(opts, path); err != nil {
		t.Fatalf("uninstallWindowsService: %v", err)
	}

	assertWindowsServiceOpen(t, deleteManager, windowsServiceUninstallAccess)
	assertWindowsServiceOpen(t, presentManager, windowsServiceDeletionInspectAccess)
	assertWindowsServiceOpen(t, absenceManager, windowsServiceDeletionInspectAccess)
	if presentHandle.closeCalls != 1 {
		t.Fatalf("verification service close calls = %d, want 1", presentHandle.closeCalls)
	}
	for index, manager := range []*fakeWindowsSCM{deleteManager, presentManager, absenceManager} {
		if manager.disconnectCalls != 1 {
			t.Fatalf("manager %d disconnect calls = %d, want 1", index, manager.disconnectCalls)
		}
	}
	if *sleepCalls != 1 {
		t.Fatalf("deletion verification sleeps = %d, want 1", *sleepCalls)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy config remains after proven deletion: %v", err)
	}
}

func TestUninstallWindowsServicePersistentMarkedDeletionFailsAndKeepsConfig(t *testing.T) {
	handle := &fakeWindowsService{states: []svc.State{svc.Stopped}}
	opts, path := writeLegacyWindowsServiceConfig(t, "must remain")
	bindRecognizedLegacyWindowsServiceConfig(handle, path)
	deleteManager := &fakeWindowsSCM{service: handle}
	pendingManagers := []*fakeWindowsSCM{
		{openErr: windows.ERROR_SERVICE_MARKED_FOR_DELETE},
		{openErr: windows.ERROR_SERVICE_MARKED_FOR_DELETE},
		{openErr: windows.ERROR_SERVICE_MARKED_FOR_DELETE},
	}
	stubWindowsSCMConnections(
		t,
		deleteManager,
		pendingManagers[0],
		pendingManagers[1],
		pendingManagers[2],
	)
	sleepCalls := stubWindowsServiceDeletionRetry(t, len(pendingManagers))

	err := uninstallWindowsService(opts, path)
	assertWindowsSCMDeletionPending(t, err, "uninstall")

	assertWindowsServiceOpen(t, deleteManager, windowsServiceUninstallAccess)
	if deleteManager.disconnectCalls != 1 || handle.closeCalls != 1 {
		t.Fatalf(
			"delete manager disconnect/service close calls = %d/%d, want 1/1",
			deleteManager.disconnectCalls,
			handle.closeCalls,
		)
	}
	for index, manager := range pendingManagers {
		assertWindowsServiceOpen(t, manager, windowsServiceDeletionInspectAccess)
		if manager.disconnectCalls != 1 {
			t.Fatalf("pending manager %d disconnect calls = %d, want 1", index, manager.disconnectCalls)
		}
	}
	if *sleepCalls != len(pendingManagers)-1 {
		t.Fatalf(
			"deletion verification sleeps = %d, want %d",
			*sleepCalls,
			len(pendingManagers)-1,
		)
	}
	assertWindowsServiceConfig(t, path, "must remain")
}

func TestUninstallWindowsServicePersistentRegistrationFailsPendingAndKeepsConfig(t *testing.T) {
	handle := &fakeWindowsService{states: []svc.State{svc.Stopped}}
	opts, path := writeLegacyWindowsServiceConfig(t, "must remain")
	bindRecognizedLegacyWindowsServiceConfig(handle, path)
	deleteManager := &fakeWindowsSCM{service: handle}
	presentHandles := []*fakeWindowsService{{}, {}}
	presentManagers := []*fakeWindowsSCM{
		{service: presentHandles[0]},
		{service: presentHandles[1]},
	}
	stubWindowsSCMConnections(t, deleteManager, presentManagers[0], presentManagers[1])
	sleepCalls := stubWindowsServiceDeletionRetry(t, len(presentManagers))

	err := uninstallWindowsService(opts, path)
	assertWindowsSCMDeletionPending(t, err, "uninstall")

	assertWindowsServiceOpen(t, deleteManager, windowsServiceUninstallAccess)
	for index, manager := range presentManagers {
		assertWindowsServiceOpen(t, manager, windowsServiceDeletionInspectAccess)
		if manager.disconnectCalls != 1 || presentHandles[index].closeCalls != 1 {
			t.Fatalf(
				"present manager %d disconnect/service close calls = %d/%d, want 1/1",
				index,
				manager.disconnectCalls,
				presentHandles[index].closeCalls,
			)
		}
	}
	if *sleepCalls != len(presentManagers)-1 {
		t.Fatalf(
			"deletion verification sleeps = %d, want %d",
			*sleepCalls,
			len(presentManagers)-1,
		)
	}
	assertWindowsServiceConfig(t, path, "must remain")
}

func TestUninstallWindowsServiceDeletionVerificationErrorsKeepConfig(t *testing.T) {
	errConnect := errors.New("verification SCM unavailable")
	errOpen := errors.New("verification access denied")
	errClose := errors.New("verification service close failed")
	errDisconnect := errors.New("verification disconnect failed")
	tests := []struct {
		name                string
		verificationManager *fakeWindowsSCM
		connectErr          error
		want                error
	}{
		{
			name:       "connect",
			connectErr: errConnect,
			want:       errConnect,
		},
		{
			name:                "open",
			verificationManager: &fakeWindowsSCM{openErr: errOpen},
			want:                errOpen,
		},
		{
			name: "close",
			verificationManager: &fakeWindowsSCM{
				service: &fakeWindowsService{closeErr: errClose},
			},
			want: errClose,
		},
		{
			name: "disconnect",
			verificationManager: &fakeWindowsSCM{
				openErr:       windows.ERROR_SERVICE_DOES_NOT_EXIST,
				disconnectErr: errDisconnect,
			},
			want: errDisconnect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handle := &fakeWindowsService{states: []svc.State{svc.Stopped}}
			opts, path := writeLegacyWindowsServiceConfig(t, "must remain")
			bindRecognizedLegacyWindowsServiceConfig(handle, path)
			deleteManager := &fakeWindowsSCM{service: handle}
			originalConnect := connectWindowsSCM
			connectCalls := 0
			connectWindowsSCM = func() (windowsSCM, error) {
				connectCalls++
				if connectCalls == 1 {
					return deleteManager, nil
				}
				if tt.connectErr != nil {
					return nil, tt.connectErr
				}
				return tt.verificationManager, nil
			}
			t.Cleanup(func() { connectWindowsSCM = originalConnect })
			stubWindowsServiceDeletionRetry(t, 1)

			err := uninstallWindowsService(opts, path)
			if err == nil {
				t.Fatal("uninstallWindowsService unexpectedly succeeded")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want wrapped %v", err, tt.want)
			}

			assertWindowsServiceOpen(t, deleteManager, windowsServiceUninstallAccess)
			if deleteManager.disconnectCalls != 1 || handle.closeCalls != 1 {
				t.Fatalf(
					"delete manager disconnect/service close calls = %d/%d, want 1/1",
					deleteManager.disconnectCalls,
					handle.closeCalls,
				)
			}
			if tt.verificationManager != nil {
				assertWindowsServiceOpen(t, tt.verificationManager, windowsServiceDeletionInspectAccess)
				if tt.verificationManager.disconnectCalls != 1 {
					t.Fatalf(
						"verification disconnect calls = %d, want 1",
						tt.verificationManager.disconnectCalls,
					)
				}
			}
			assertWindowsServiceConfig(t, path, "must remain")
		})
	}
}

func TestUninstallWindowsServiceDeleteHandleCleanupErrorsKeepConfig(t *testing.T) {
	errClose := errors.New("deleted service close failed")
	errDisconnect := errors.New("delete SCM disconnect failed")
	tests := []struct {
		name          string
		handle        *fakeWindowsService
		disconnectErr error
		want          error
	}{
		{
			name:   "service close",
			handle: &fakeWindowsService{states: []svc.State{svc.Stopped}, closeErr: errClose},
			want:   errClose,
		},
		{
			name:          "SCM disconnect",
			handle:        &fakeWindowsService{states: []svc.State{svc.Stopped}},
			disconnectErr: errDisconnect,
			want:          errDisconnect,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, path := writeLegacyWindowsServiceConfig(t, "must remain")
			bindRecognizedLegacyWindowsServiceConfig(tt.handle, path)
			manager := &fakeWindowsSCM{
				service:       tt.handle,
				disconnectErr: tt.disconnectErr,
			}
			stubWindowsSCMConnections(t, manager)
			stubWindowsServiceDeletionRetry(t, 1)

			err := uninstallWindowsService(opts, path)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want wrapped %v", err, tt.want)
			}

			assertWindowsServiceOpen(t, manager, windowsServiceUninstallAccess)
			if tt.handle.deleteCalls != 1 || tt.handle.closeCalls != 1 {
				t.Fatalf(
					"delete/close calls = %d/%d, want 1/1",
					tt.handle.deleteCalls,
					tt.handle.closeCalls,
				)
			}
			if manager.disconnectCalls != 1 {
				t.Fatalf("disconnect calls = %d, want 1", manager.disconnectCalls)
			}
			assertWindowsServiceConfig(t, path, "must remain")
		})
	}
}

func TestWindowsSCMMarkedForDeleteFailsClosedAcrossRemediationOperations(t *testing.T) {
	type operationResult struct {
		err       error
		installed bool
	}
	tests := []struct {
		name       string
		operation  string
		wantAccess uint32
		run        func(opts InstallOptions, path string) operationResult
	}{
		{
			name:       "inspect",
			operation:  "inspection",
			wantAccess: windowsServiceInspectAccess,
			run: func(opts InstallOptions, _ string) operationResult {
				status, err := inspectWindowsService(opts)
				return operationResult{err: err, installed: status.Installed}
			},
		},
		{
			name:       "stop",
			operation:  "stop",
			wantAccess: windowsServiceStopAccess,
			run: func(opts InstallOptions, _ string) operationResult {
				return operationResult{err: stopWindowsService(opts)}
			},
		},
		{
			name:       "uninstall",
			operation:  "uninstall",
			wantAccess: windowsServiceUninstallAccess,
			run: func(opts InstallOptions, path string) operationResult {
				return operationResult{err: uninstallWindowsService(opts, path)}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := &fakeWindowsSCM{openErr: windows.ERROR_SERVICE_MARKED_FOR_DELETE}
			originalConnect := connectWindowsSCM
			connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
			t.Cleanup(func() { connectWindowsSCM = originalConnect })

			opts, path := writeLegacyWindowsServiceConfig(t, "must remain")

			result := tt.run(opts, path)
			assertWindowsSCMDeletionPending(t, result.err, tt.operation)
			assertWindowsServiceOpen(t, manager, tt.wantAccess)
			if tt.name == "inspect" && !result.installed {
				t.Fatal("marked-for-delete service was reported absent")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("marked-for-delete operation removed config: %v", err)
			}
			if string(raw) != "must remain" {
				t.Fatalf("marked-for-delete operation changed config to %q", raw)
			}
		})
	}
}

func TestUninstallWindowsServiceDeleteRaceFailsClosedAndKeepsConfig(t *testing.T) {
	handle := &fakeWindowsService{
		states:    []svc.State{svc.Stopped},
		deleteErr: windows.ERROR_SERVICE_MARKED_FOR_DELETE,
	}
	opts, path := writeLegacyWindowsServiceConfig(t, "must remain")
	bindRecognizedLegacyWindowsServiceConfig(handle, path)
	manager := &fakeWindowsSCM{service: handle}
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	err := uninstallWindowsService(opts, path)
	assertWindowsSCMDeletionPending(t, err, "uninstall")
	assertWindowsServiceOpen(t, manager, windowsServiceUninstallAccess)
	if handle.deleteCalls != 1 || handle.closeCalls != 1 {
		t.Fatalf("delete/close calls = %d/%d, want 1/1", handle.deleteCalls, handle.closeCalls)
	}
	if raw, readErr := os.ReadFile(path); readErr != nil || string(raw) != "must remain" {
		t.Fatalf("delete race changed config: raw=%q err=%v", raw, readErr)
	}
}

func stubWindowsSCMConnections(t *testing.T, managers ...*fakeWindowsSCM) {
	t.Helper()
	originalConnect := connectWindowsSCM
	index := 0
	connectWindowsSCM = func() (windowsSCM, error) {
		if index >= len(managers) {
			return nil, fmt.Errorf("unexpected SCM connection %d", index+1)
		}
		manager := managers[index]
		index++
		return manager, nil
	}
	t.Cleanup(func() { connectWindowsSCM = originalConnect })
}

func stubWindowsServiceDeletionRetry(t *testing.T, attempts int) *int {
	t.Helper()
	originalAttempts := windowsServiceDeletionAttempts
	originalSleep := windowsServiceDeletionSleep
	sleepCalls := 0
	windowsServiceDeletionAttempts = attempts
	windowsServiceDeletionSleep = func(time.Duration) {
		sleepCalls++
	}
	t.Cleanup(func() {
		windowsServiceDeletionAttempts = originalAttempts
		windowsServiceDeletionSleep = originalSleep
	})
	return &sleepCalls
}

func assertWindowsServiceConfig(t *testing.T, path, want string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read service config: %v", err)
	}
	if string(raw) != want {
		t.Fatalf("service config = %q, want %q", raw, want)
	}
}

func assertWindowsServiceOpen(t *testing.T, manager *fakeWindowsSCM, wantAccess uint32) {
	t.Helper()
	if manager.openCalls != 1 {
		t.Fatalf("OpenService calls = %d, want 1", manager.openCalls)
	}
	if len(manager.openNames) != 1 || manager.openNames[0] != UnitName(BackendWindowsSCM) {
		t.Fatalf("OpenService names = %v, want [%s]", manager.openNames, UnitName(BackendWindowsSCM))
	}
	if len(manager.openAccesses) != 1 || manager.openAccesses[0] != wantAccess {
		t.Fatalf("OpenService access = %v, want [%#x]", manager.openAccesses, wantAccess)
	}
}

func recognizedLegacyWindowsServiceConfig() mgr.Config {
	path, err := expectedLegacyWindowsServiceConfigPath(InstallOptions{})
	if err != nil {
		panic(err)
	}
	return recognizedLegacyWindowsServiceConfigForPath(path)
}

func recognizedLegacyWindowsServiceConfigForPath(path string) mgr.Config {
	return mgr.Config{
		ServiceType: windows.SERVICE_WIN32_OWN_PROCESS,
		DisplayName: windowsServiceHistoricalDisplayName,
		Description: windowsServiceHistoricalDescription,
		BinaryPathName: windows.ComposeCommandLine([]string{
			`C:\Program Files\radioactive-ralph\radioactive_ralph.exe`,
			"--supervisor",
			"--windows-service-config",
			path,
		}),
	}
}

func writeLegacyWindowsServiceConfig(
	t *testing.T,
	content string,
) (InstallOptions, string) {
	t.Helper()
	opts := InstallOptions{HomeDir: t.TempDir()}
	path := UnitPath(BackendWindowsSCM, opts.HomeDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll legacy config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile legacy config: %v", err)
	}
	return opts, path
}

func bindRecognizedLegacyWindowsServiceConfig(
	handle *fakeWindowsService,
	path string,
) {
	handle.config = recognizedLegacyWindowsServiceConfigForPath(path)
	handle.configSet = true
}

func assertWindowsSCMDeletionPending(t *testing.T, err error, operation string) {
	t.Helper()
	if !errors.Is(err, ErrWindowsSCMDeletionPending) {
		t.Fatalf("error = %v, want ErrWindowsSCMDeletionPending", err)
	}
	var typed *WindowsSCMDeletionPendingError
	if !errors.As(err, &typed) {
		t.Fatalf("error type = %T, want *WindowsSCMDeletionPendingError", err)
	}
	if typed.ServiceName != UnitName(BackendWindowsSCM) || typed.Operation != operation {
		t.Fatalf("pending error = %#v, want service %q operation %q", typed, UnitName(BackendWindowsSCM), operation)
	}
	for _, clause := range []string{
		"marked for deletion",
		"process and registration may still exist",
		"wait for SCM deletion to finish",
		"reboot Windows",
		"retry",
	} {
		if !strings.Contains(err.Error(), clause) {
			t.Fatalf("pending error %q missing %q", err, clause)
		}
	}
}

func assertWindowsSCMDisabled(t *testing.T, err error, operation WindowsSCMOperation) {
	t.Helper()
	if !errors.Is(err, ErrWindowsSCMDisabled) {
		t.Fatalf("error = %v, want ErrWindowsSCMDisabled", err)
	}
	var typed *WindowsSCMDisabledError
	if !errors.As(err, &typed) {
		t.Fatalf("error type = %T, want *WindowsSCMDisabledError", err)
	}
	if typed.Operation != operation {
		t.Fatalf("operation = %q, want %q", typed.Operation, operation)
	}
	for _, clause := range []string{
		"native Windows SCM service",
		"is disabled",
		"radioactive_ralph --supervisor",
		"WSL2",
		"systemd --user",
	} {
		if !strings.Contains(err.Error(), clause) {
			t.Fatalf("disabled error %q missing %q", err, clause)
		}
	}
}
