//go:build !windows

package main

import "fmt"

// ServiceWindowsRunCmd exists so the command tree stays buildable on every
// platform; only Windows uses it.
type ServiceWindowsRunCmd struct{}

func (c *ServiceWindowsRunCmd) Run(_ *runContext) error {
	return fmt.Errorf("service run-windows is only available on native Windows")
}
