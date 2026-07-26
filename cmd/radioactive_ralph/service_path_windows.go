//go:build windows

package main

// Native Windows SCM installation is disabled. Keep the platform path policy
// fail-closed as defense in depth so no future caller can accidentally revive
// installer-derived service configuration through this helper.
func servicePathDirAllowed(string) bool {
	return false
}
