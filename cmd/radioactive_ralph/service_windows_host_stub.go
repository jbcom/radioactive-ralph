//go:build !windows

package main

func maybeRunWindowsServiceHost(_ []string) (bool, error) {
	return false, nil
}
