//go:build windows

package main

// A newly created Windows SCM service uses LocalSystem by default, while
// reconciliation preserves an existing administrator-configured account.
// Inferring the installing administrator's PATH across either potentially
// different identity is unsafe. Leave PATH absent so SCM supplies its own
// service environment; an explicit --env PATH remains an
// administrator-controlled override.
func servicePathDirAllowed(string) bool {
	return false
}
