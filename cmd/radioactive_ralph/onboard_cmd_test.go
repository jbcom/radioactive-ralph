package main

import (
	"strings"
	"testing"
)

func TestWindowsNoSupervisorMessageDoesNotOfferSCMInstall(t *testing.T) {
	message := noSupervisorMessageFor("windows")
	for _, clause := range []string{
		"radioactive_ralph --supervisor",
		"provider PTYs are unsupported",
		"WSL2",
		"systemd --user",
	} {
		if !strings.Contains(message, clause) {
			t.Fatalf("Windows no-supervisor message %q missing %q", message, clause)
		}
	}
	if strings.Contains(message, "service install") {
		t.Fatalf("Windows no-supervisor message offers disabled SCM install: %q", message)
	}
}

func TestUnixNoSupervisorMessageRetainsServiceInstall(t *testing.T) {
	if message := noSupervisorMessageFor("linux"); !strings.Contains(message, "radioactive_ralph service install") {
		t.Fatalf("Linux no-supervisor message lost service install guidance: %q", message)
	}
}
