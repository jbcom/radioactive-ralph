//go:build darwin

package main

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDarwinHomebrewPathAllowed(t *testing.T) {
	const effectiveUID = 501
	trusted := map[string]servicePathMetadata{
		"/": {
			mode:      0o755,
			uid:       0,
			gid:       darwinWheelGroup,
			directory: true,
		},
		"/opt": {
			mode:      0o755,
			uid:       0,
			gid:       0,
			directory: true,
		},
		"/opt/homebrew": {
			mode:      0o755,
			uid:       effectiveUID,
			gid:       darwinAdminGroup,
			directory: true,
		},
		darwinArmHomebrewBin: {
			mode:      0o775,
			uid:       effectiveUID,
			gid:       darwinAdminGroup,
			directory: true,
		},
	}
	lookup := func(
		overrides map[string]servicePathMetadata,
		unavailable map[string]bool,
	) func(string) (servicePathMetadata, bool) {
		return func(path string) (servicePathMetadata, bool) {
			if unavailable[path] {
				return servicePathMetadata{}, false
			}
			if override, ok := overrides[path]; ok {
				return override, true
			}
			metadata, ok := trusted[path]
			return metadata, ok
		}
	}

	tests := []struct {
		name        string
		candidate   string
		overrides   map[string]servicePathMetadata
		unavailable map[string]bool
		want        bool
	}{
		{
			name:      "exact trusted Homebrew bin",
			candidate: darwinArmHomebrewBin,
			want:      true,
		},
		{
			name:      "different candidate cannot use exception",
			candidate: "/tmp/homebrew/bin",
		},
		{
			name:      "foreign leaf owner",
			candidate: darwinArmHomebrewBin,
			overrides: map[string]servicePathMetadata{
				darwinArmHomebrewBin: {
					mode:      0o775,
					uid:       502,
					gid:       darwinAdminGroup,
					directory: true,
				},
			},
		},
		{
			name:      "wrong leaf group",
			candidate: darwinArmHomebrewBin,
			overrides: map[string]servicePathMetadata{
				darwinArmHomebrewBin: {
					mode:      0o775,
					uid:       effectiveUID,
					gid:       20,
					directory: true,
				},
			},
		},
		{
			name:      "world writable leaf",
			candidate: darwinArmHomebrewBin,
			overrides: map[string]servicePathMetadata{
				darwinArmHomebrewBin: {
					mode:      0o777,
					uid:       effectiveUID,
					gid:       darwinAdminGroup,
					directory: true,
				},
			},
		},
		{
			name:      "symlinked leaf",
			candidate: darwinArmHomebrewBin,
			overrides: map[string]servicePathMetadata{
				darwinArmHomebrewBin: {
					mode:      0o775,
					uid:       effectiveUID,
					gid:       darwinAdminGroup,
					directory: true,
					symlink:   true,
				},
			},
		},
		{
			name:      "group writable opt",
			candidate: darwinArmHomebrewBin,
			overrides: map[string]servicePathMetadata{
				"/opt": {
					mode:      0o775,
					uid:       0,
					gid:       darwinAdminGroup,
					directory: true,
				},
			},
		},
		{
			name:      "symlinked opt",
			candidate: darwinArmHomebrewBin,
			overrides: map[string]servicePathMetadata{
				"/opt": {
					mode:      0o755,
					uid:       0,
					gid:       darwinWheelGroup,
					directory: true,
					symlink:   true,
				},
			},
		},
		{
			name:      "foreign owned prefix",
			candidate: darwinArmHomebrewBin,
			overrides: map[string]servicePathMetadata{
				"/opt/homebrew": {
					mode:      0o755,
					uid:       502,
					gid:       darwinAdminGroup,
					directory: true,
				},
			},
		},
		{
			name:      "world writable prefix",
			candidate: darwinArmHomebrewBin,
			overrides: map[string]servicePathMetadata{
				"/opt/homebrew": {
					mode:      0o777,
					uid:       effectiveUID,
					gid:       darwinAdminGroup,
					directory: true,
				},
			},
		},
		{
			name:      "unprivileged prefix group",
			candidate: darwinArmHomebrewBin,
			overrides: map[string]servicePathMetadata{
				"/opt/homebrew": {
					mode:      0o755,
					uid:       effectiveUID,
					gid:       20,
					directory: true,
				},
			},
		},
		{
			name:      "symlinked prefix",
			candidate: darwinArmHomebrewBin,
			overrides: map[string]servicePathMetadata{
				"/opt/homebrew": {
					mode:      0o755,
					uid:       effectiveUID,
					gid:       darwinAdminGroup,
					directory: true,
					symlink:   true,
				},
			},
		},
		{
			name:        "missing prefix",
			candidate:   darwinArmHomebrewBin,
			unavailable: map[string]bool{"/opt/homebrew": true},
		},
		{
			name:        "missing leaf",
			candidate:   darwinArmHomebrewBin,
			unavailable: map[string]bool{darwinArmHomebrewBin: true},
		},
		{
			name:      "wrong root group",
			candidate: darwinArmHomebrewBin,
			overrides: map[string]servicePathMetadata{
				"/": {
					mode:      0o755,
					uid:       0,
					gid:       darwinAdminGroup,
					directory: true,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := darwinHomebrewPathAllowed(
				tt.candidate,
				"arm64",
				effectiveUID,
				lookup(tt.overrides, tt.unavailable),
			)
			if got != tt.want {
				t.Fatalf("darwinHomebrewPathAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDarwinIntelHomebrewPathAllowed(t *testing.T) {
	const effectiveUID = 501
	trusted := map[string]servicePathMetadata{
		"/": {
			mode:      0o755,
			uid:       0,
			gid:       darwinWheelGroup,
			directory: true,
		},
		"/usr": {
			mode:      0o755,
			uid:       0,
			gid:       darwinWheelGroup,
			directory: true,
		},
		"/usr/local": {
			mode:      0o775,
			uid:       effectiveUID,
			gid:       darwinAdminGroup,
			directory: true,
		},
		darwinIntelHomebrewBin: {
			mode:      0o775,
			uid:       effectiveUID,
			gid:       darwinAdminGroup,
			directory: true,
		},
	}
	allowed := func(
		architecture string,
		candidate string,
		overrides map[string]servicePathMetadata,
	) bool {
		return darwinHomebrewPathAllowed(
			candidate,
			architecture,
			effectiveUID,
			func(path string) (servicePathMetadata, bool) {
				if override, ok := overrides[path]; ok {
					return override, true
				}
				metadata, ok := trusted[path]
				return metadata, ok
			},
		)
	}

	if !allowed("amd64", darwinIntelHomebrewBin, nil) {
		t.Fatal("trusted Intel Homebrew path rejected")
	}
	if allowed("arm64", darwinIntelHomebrewBin, nil) {
		t.Fatal("Intel Homebrew exception accepted on arm64")
	}
	if allowed("amd64", "/usr/local/sbin", nil) {
		t.Fatal("Intel Homebrew exception accepted a lookalike path")
	}
	if allowed("amd64", darwinIntelHomebrewBin, map[string]servicePathMetadata{
		"/usr/local": {
			mode:      0o775,
			uid:       502,
			gid:       darwinAdminGroup,
			directory: true,
		},
	}) {
		t.Fatal("Intel Homebrew exception accepted a foreign-owned prefix")
	}
	if allowed("amd64", darwinIntelHomebrewBin, map[string]servicePathMetadata{
		darwinIntelHomebrewBin: {
			mode:      0o777,
			uid:       effectiveUID,
			gid:       darwinAdminGroup,
			directory: true,
		},
	}) {
		t.Fatal("Intel Homebrew exception accepted a world-writable bin")
	}
	if allowed("amd64", darwinIntelHomebrewBin, map[string]servicePathMetadata{
		darwinIntelHomebrewBin: {
			mode:      0o775,
			uid:       effectiveUID,
			gid:       20,
			directory: true,
		},
	}) {
		t.Fatal("Intel Homebrew exception accepted a group-writable non-admin bin")
	}
	if allowed("amd64", darwinIntelHomebrewBin, map[string]servicePathMetadata{
		"/usr/local": {
			mode:      0o775,
			uid:       effectiveUID,
			gid:       darwinAdminGroup,
			directory: true,
			symlink:   true,
		},
	}) {
		t.Fatal("Intel Homebrew exception accepted a symlinked prefix")
	}
}

func TestServiceExecutionPathIncludesTrustedDarwinHomebrewBin(t *testing.T) {
	candidate := darwinArmHomebrewBin
	if runtime.GOARCH == "amd64" {
		candidate = darwinIntelHomebrewBin
	}
	if !servicePathDirAllowed(candidate) {
		t.Skipf("%s does not match the supported Homebrew ownership contract on this host", candidate)
	}
	got := serviceExecutionPath("/usr/local/bin/radioactive_ralph", "")
	for _, entry := range filepath.SplitList(got) {
		if entry == candidate {
			return
		}
	}
	t.Fatalf("serviceExecutionPath() = %q, want %s", got, candidate)
}

func TestServiceInstallPersistsTrustedDarwinHomebrewBin(t *testing.T) {
	candidate := darwinArmHomebrewBin
	if runtime.GOARCH == "amd64" {
		candidate = darwinIntelHomebrewBin
	}
	if !servicePathDirAllowed(candidate) {
		t.Skipf("%s does not match the supported Homebrew ownership contract on this host", candidate)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin")
	stubSupervisorServiceStart(t)

	cmd := newTestRootCmd(context.Background())
	cmd.SetArgs([]string{
		"service", "install",
		"--bin", "/usr/local/bin/radioactive_ralph",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("service install: %v", err)
	}
	assertInstalledUnitContains(t, home, candidate)
}
