//go:build windows

package adapters

import "fmt"

func cleanupOpenCodeRuntime(_, _ string) error {
	return fmt.Errorf("secure OpenCode runtime cleanup is unsupported on Windows")
}
