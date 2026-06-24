package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/service"
)

func TestServiceListPattern(t *testing.T) {
	tests := []struct {
		name    string
		backend service.Backend
		home    string
		want    string
		wantErr string
	}{
		{
			name:    "launchd",
			backend: service.BackendLaunchd,
			home:    "/tmp/home",
			want:    filepath.Join("/tmp/home", "Library", "LaunchAgents", "jbcom.radioactive-ralph.*.plist"),
		},
		{
			name:    "systemd",
			backend: service.BackendSystemdUser,
			home:    "/tmp/home",
			want:    filepath.Join("/tmp/home", ".config", "systemd", "user", "radioactive_ralph-*.service"),
		},
		{
			name:    "windows",
			backend: service.BackendWindowsSCM,
			home:    `C:\Users\me`,
			want:    filepath.Join(`C:\Users\me`, "AppData", "Local", "radioactive-ralph", "services", "radioactive_ralph-*.json"),
		},
		{
			name:    "unsupported",
			backend: service.BackendUnsupported,
			home:    "/tmp/home",
			wantErr: "service list not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := serviceListPattern(tt.backend, tt.home)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("serviceListPattern() err = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("serviceListPattern() err = %v", err)
			}
			if got != tt.want {
				t.Fatalf("serviceListPattern() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServiceStatusInvocation(t *testing.T) {
	repo := "/Users/me/src/repo"
	tests := []struct {
		name     string
		backend  service.Backend
		wantBin  string
		wantArgs []string
		wantErr  string
	}{
		{
			name:    "launchd",
			backend: service.BackendLaunchd,
			wantBin: "launchctl",
			wantArgs: []string{
				"list",
				service.UnitName(service.BackendLaunchd, repo),
			},
		},
		{
			name:    "systemd",
			backend: service.BackendSystemdUser,
			wantBin: "systemctl",
			wantArgs: []string{
				"--user",
				"status",
				service.UnitName(service.BackendSystemdUser, repo) + ".service",
			},
		},
		{
			name:    "windows",
			backend: service.BackendWindowsSCM,
			wantBin: "sc.exe",
			wantArgs: []string{
				"query",
				service.UnitName(service.BackendWindowsSCM, repo),
			},
		},
		{
			name:    "unsupported",
			backend: service.BackendUnsupported,
			wantErr: "service status not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBin, gotArgs, err := serviceStatusInvocation(tt.backend, repo)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("serviceStatusInvocation() err = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("serviceStatusInvocation() err = %v", err)
			}
			if gotBin != tt.wantBin {
				t.Fatalf("serviceStatusInvocation() bin = %q, want %q", gotBin, tt.wantBin)
			}
			if !reflect.DeepEqual(gotArgs, tt.wantArgs) {
				t.Fatalf("serviceStatusInvocation() args = %#v, want %#v", gotArgs, tt.wantArgs)
			}
		})
	}
}

func TestParseWindowsServiceHostArgs(t *testing.T) {
	args := []string{
		"radioactive_ralph",
		"service",
		"run-windows",
		"--repo-root", `C:\repo`,
		"--service-name=radioactive_ralph-repo-123",
		"--config-path", `C:\state\svc.json`,
	}
	got, handled, err := parseWindowsServiceHostArgs(args)
	if err != nil {
		t.Fatalf("parseWindowsServiceHostArgs() err = %v", err)
	}
	if !handled {
		t.Fatal("parseWindowsServiceHostArgs() did not handle run-windows args")
	}
	if got.RepoRoot != `C:\repo` {
		t.Fatalf("RepoRoot = %q", got.RepoRoot)
	}
	if got.ServiceName != "radioactive_ralph-repo-123" {
		t.Fatalf("ServiceName = %q", got.ServiceName)
	}
	if got.ConfigPath != `C:\state\svc.json` {
		t.Fatalf("ConfigPath = %q", got.ConfigPath)
	}
}

func TestParseWindowsServiceHostArgsRejectsUnknownArg(t *testing.T) {
	_, handled, err := parseWindowsServiceHostArgs([]string{
		"radioactive_ralph",
		"service",
		"run-windows",
		"--repo-root", `C:\repo`,
		"--wat",
	})
	if !handled {
		t.Fatal("parseWindowsServiceHostArgs() did not handle run-windows args")
	}
	if err == nil {
		t.Fatal("parseWindowsServiceHostArgs() err = nil, want error")
	}
	if !strings.Contains(err.Error(), "unknown run-windows argument") {
		t.Fatalf("parseWindowsServiceHostArgs() err = %v", err)
	}
}
