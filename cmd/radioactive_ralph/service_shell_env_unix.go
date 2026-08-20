//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func captureLoginShellEnvRaw() ([]byte, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	//nolint:gosec // shell is the user's login shell from $SHELL; the command is a fixed literal ("env"), not user input
	cmd := exec.Command(shell, "-l", "-c", "env")
	cmd.Stdin = nil
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run %s -l -c env: %w", shell, err)
	}
	return output, nil
}