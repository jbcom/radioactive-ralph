//go:build windows

package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type fakeWindowsSCM struct {
	service    *fakeWindowsService
	openErr    error
	created    bool
	createArgs []string
}

func (f *fakeWindowsSCM) OpenService(string) (windowsServiceHandle, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.service, nil
}

func (f *fakeWindowsSCM) CreateService(
	_,
	_ string,
	_ mgr.Config,
	args ...string,
) (windowsServiceHandle, error) {
	f.created = true
	f.createArgs = append([]string(nil), args...)
	return f.service, nil
}

func (*fakeWindowsSCM) Disconnect() error { return nil }

type fakeWindowsService struct {
	states       []svc.State
	queryIndex   int
	config       mgr.Config
	updated      mgr.Config
	startCalls   int
	controlCalls []svc.Cmd
	deleteCalls  int
	closeCalls   int
}

func (f *fakeWindowsService) Close() error {
	f.closeCalls++
	return nil
}

func (f *fakeWindowsService) Config() (mgr.Config, error) {
	return f.config, nil
}

func (f *fakeWindowsService) UpdateConfig(config mgr.Config) error {
	f.updated = config
	return nil
}

func (f *fakeWindowsService) Query() (svc.Status, error) {
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

func (f *fakeWindowsService) Start(...string) error {
	f.startCalls++
	return nil
}

func (f *fakeWindowsService) Control(cmd svc.Cmd) (svc.Status, error) {
	f.controlCalls = append(f.controlCalls, cmd)
	return svc.Status{State: svc.StopPending}, nil
}

func (f *fakeWindowsService) Delete() error {
	f.deleteCalls++
	return nil
}

func TestInstallWindowsServiceUpdatesRestartsAndPersistsEnvironment(t *testing.T) {
	handle := &fakeWindowsService{
		states: []svc.State{svc.Running, svc.Stopped, svc.Stopped, svc.Running},
		config: mgr.Config{
			BinaryPathName: `C:\old\radioactive_ralph.exe --supervisor`,
			StartType:      mgr.StartManual,
		},
	}
	manager := &fakeWindowsSCM{service: handle}
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	path := filepath.Join(t.TempDir(), "service.json")
	got, err := installWindowsService(InstallOptions{
		RalphBin: `C:\Program Files\radioactive-ralph\radioactive_ralph.exe`,
		ExtraEnv: map[string]string{
			"RALPH_STATE_DIR":    `C:\Users\alice\AppData\Local\radioactive-ralph`,
			"RALPH_MAX_PARALLEL": "16",
		},
	}, path)
	if err != nil {
		t.Fatalf("installWindowsService: %v", err)
	}
	if got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
	if manager.created {
		t.Fatal("existing service was recreated instead of updated")
	}
	if len(handle.controlCalls) != 1 || handle.controlCalls[0] != svc.Stop {
		t.Fatalf("stop controls = %v, want [Stop]", handle.controlCalls)
	}
	if handle.startCalls != 1 {
		t.Fatalf("start calls = %d, want 1", handle.startCalls)
	}
	if handle.updated.StartType != mgr.StartAutomatic {
		t.Fatalf("StartType = %d, want automatic", handle.updated.StartType)
	}
	if !strings.Contains(handle.updated.BinaryPathName, "--windows-service-config") ||
		!strings.Contains(handle.updated.BinaryPathName, path) {
		t.Fatalf("updated BinaryPathName does not carry config path: %q", handle.updated.BinaryPathName)
	}
	config, err := LoadWindowsServiceConfig(path)
	if err != nil {
		t.Fatalf("LoadWindowsServiceConfig: %v", err)
	}
	if config.ExtraEnv["RALPH_MAX_PARALLEL"] != "16" {
		t.Fatalf("persisted ExtraEnv = %#v", config.ExtraEnv)
	}
}

func TestInstallWindowsServiceCreatesAndWaitsForRunning(t *testing.T) {
	handle := &fakeWindowsService{
		states: []svc.State{svc.Stopped, svc.StartPending, svc.Running},
	}
	manager := &fakeWindowsSCM{
		service: handle,
		openErr: windows.ERROR_SERVICE_DOES_NOT_EXIST,
	}
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	path := filepath.Join(t.TempDir(), "service.json")
	if _, err := installWindowsService(InstallOptions{
		RalphBin: `C:\radioactive_ralph.exe`,
	}, path); err != nil {
		t.Fatalf("installWindowsService: %v", err)
	}
	if !manager.created {
		t.Fatal("missing service was not created")
	}
	wantArgs := WindowsServiceArgsForConfig(path)
	if strings.Join(manager.createArgs, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("CreateService args = %#v, want %#v", manager.createArgs, wantArgs)
	}
	if handle.startCalls != 1 {
		t.Fatalf("start calls = %d, want 1", handle.startCalls)
	}
}

func TestUninstallWindowsServiceStopsWaitsDeletesAndIsIdempotent(t *testing.T) {
	handle := &fakeWindowsService{
		states: []svc.State{svc.Running, svc.StopPending, svc.Stopped},
	}
	manager := &fakeWindowsSCM{service: handle}
	originalConnect := connectWindowsSCM
	connectWindowsSCM = func() (windowsSCM, error) { return manager, nil }
	t.Cleanup(func() { connectWindowsSCM = originalConnect })

	path := filepath.Join(t.TempDir(), "service.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := uninstallWindowsService(InstallOptions{}, path); err != nil {
		t.Fatalf("uninstallWindowsService: %v", err)
	}
	if handle.deleteCalls != 1 {
		t.Fatalf("delete calls = %d, want 1", handle.deleteCalls)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("config remains after uninstall: %v", err)
	}

	missing := &fakeWindowsSCM{openErr: windows.ERROR_SERVICE_DOES_NOT_EXIST}
	connectWindowsSCM = func() (windowsSCM, error) { return missing, nil }
	if err := uninstallWindowsService(InstallOptions{}, path); err != nil {
		t.Fatalf("second uninstall should be idempotent: %v", err)
	}
}
