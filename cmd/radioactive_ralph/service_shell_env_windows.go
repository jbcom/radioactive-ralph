//go:build windows

package main

import "os"

// captureLoginShellEnvRaw returns the current process environment on Windows.
// Unlike Unix, where launchd/systemd start services with a minimal environment,
// Windows loads the user's environment block at logon and every process
// (including SCM services) inherits it. There is no "login shell" concept;
// the process environment IS the full user environment, so we return it as-is.
func captureLoginShellEnvRaw() ([]byte, error) {
	out := make([]byte, 0, 4096)
	for _, kv := range os.Environ() {
		out = append(out, []byte(kv)...)
		out = append(out, '\n')
	}
	return out, nil
}