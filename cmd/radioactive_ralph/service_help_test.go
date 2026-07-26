package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNativeWindowsServiceHelpStatesSupportBoundary(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"install", "--help"},
		{"status", "--help"},
		{"uninstall", "--help"},
	} {
		cmd := newServiceCmdForPlatform("windows")
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("service %s help: %v", strings.Join(args, " "), err)
		}

		help := out.String()
		for _, clause := range []string{"Native Windows", "WSL2", "provider-backed execution"} {
			if !strings.Contains(help, clause) {
				t.Errorf("service %s help missing %q:\n%s", strings.Join(args, " "), clause, help)
			}
		}
		if args[0] == "--help" || args[0] == "install" {
			for _, clause := range []string{"install/start", "unsupported", "radioactive_ralph --supervisor"} {
				if !strings.Contains(help, clause) {
					t.Errorf("service %s help missing install boundary %q:\n%s", strings.Join(args, " "), clause, help)
				}
			}
		} else if !strings.Contains(help, "legacy") || !strings.Contains(help, "remediation") {
			t.Errorf("service %s help must identify the legacy-remediation scope:\n%s", strings.Join(args, " "), help)
		}
	}
}

func TestSupportedPlatformServiceHelpRetainsInstallContract(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		cmd := newServiceCmdForPlatform(goos)
		install, _, err := cmd.Find([]string{"install"})
		if err != nil {
			t.Fatalf("%s find service install: %v", goos, err)
		}
		if !strings.Contains(install.Short, "Install the supervisor") {
			t.Errorf("%s install help lost supported service contract: %q", goos, install.Short)
		}
		for _, forbidden := range []string{"unsupported", "legacy", "WSL2"} {
			if strings.Contains(cmd.Short+"\n"+install.Short+"\n"+install.Long, forbidden) {
				t.Errorf("%s service help unexpectedly contains Windows-only %q guidance", goos, forbidden)
			}
		}
	}
}
