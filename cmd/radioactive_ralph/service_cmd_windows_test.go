//go:build windows

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/jbcom/radioactive-ralph/internal/service"
)

func TestServiceInstallCommandRejectsNativeWindowsSCM(t *testing.T) {
	cmd := newServiceInstallCmd()
	cmd.SetArgs([]string{"--bin", `C:\radioactive-ralph\radioactive_ralph.exe`})

	err := cmd.Execute()
	if !errors.Is(err, service.ErrWindowsSCMDisabled) {
		t.Fatalf("service install error = %v, want ErrWindowsSCMDisabled", err)
	}
	var typed *service.WindowsSCMDisabledError
	if !errors.As(err, &typed) || typed.Operation != service.WindowsSCMOperationInstall {
		t.Fatalf("service install typed error = %#v (%v)", typed, err)
	}
	for _, clause := range []string{
		"native Windows SCM service installation is disabled",
		"radioactive_ralph --supervisor",
		"WSL2",
		"systemd --user",
	} {
		if !strings.Contains(err.Error(), clause) {
			t.Fatalf("service install error %q missing %q", err, clause)
		}
	}
}
