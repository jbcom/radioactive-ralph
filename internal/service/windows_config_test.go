package service

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWindowsServiceArgsForConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service config.json")
	args := WindowsServiceArgsForConfig(path)
	want := []string{"--supervisor", "--windows-service-config", path}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("WindowsServiceArgsForConfig() = %#v, want %#v", args, want)
	}
	got, err := WindowsServiceConfigPath(args)
	if err != nil {
		t.Fatalf("WindowsServiceConfigPath: %v", err)
	}
	if got != path {
		t.Fatalf("WindowsServiceConfigPath = %q, want %q", got, path)
	}
}

func TestWindowsServiceConfigPathAcceptsEqualsForm(t *testing.T) {
	got, err := WindowsServiceConfigPath([]string{
		"--supervisor",
		"--windows-service-config=C:\\ProgramData\\radioactive-ralph\\service.json",
	})
	if err != nil {
		t.Fatalf("WindowsServiceConfigPath: %v", err)
	}
	if got != `C:\ProgramData\radioactive-ralph\service.json` {
		t.Fatalf("WindowsServiceConfigPath = %q", got)
	}
}

func TestWindowsServiceConfigPathRejectsMissingPath(t *testing.T) {
	for _, args := range [][]string{
		{"--supervisor"},
		{"--supervisor", "--windows-service-config"},
		{"--supervisor", "--windows-service-config="},
	} {
		if _, err := WindowsServiceConfigPath(args); err == nil {
			t.Fatalf("WindowsServiceConfigPath(%q) succeeded, want error", args)
		}
	}
}

func TestLoadWindowsServiceConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.json")
	raw, err := MarshalWindowsServiceConfig(InstallOptions{
		ExtraEnv: map[string]string{
			"RALPH_STATE_DIR":    `C:\Users\alice\AppData\Local\radioactive-ralph`,
			"RALPH_MAX_PARALLEL": "16",
		},
	})
	if err != nil {
		t.Fatalf("MarshalWindowsServiceConfig: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := LoadWindowsServiceConfig(path)
	if err != nil {
		t.Fatalf("LoadWindowsServiceConfig: %v", err)
	}
	if got := cfg.ExtraEnv["RALPH_MAX_PARALLEL"]; got != "16" {
		t.Fatalf("RALPH_MAX_PARALLEL = %q, want 16", got)
	}
}
