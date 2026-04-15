package mcp

import (
	"fmt"
	"os"
)

// errVariantUnknown signals the caller passed a variant_id we don't
// have in the pool. Returned to the MCP client as a tool-call error.
func errVariantUnknown(id string) error {
	return fmt.Errorf("mcp: no variant with id %q in this session's pool", id)
}

// ralphBinPath returns the absolute path of the running
// radioactive_ralph binary so variantpool.Spawn can re-exec it as
// the supervisor entry. Falls back to "radioactive_ralph" on
// $PATH if /proc/self/exe-style resolution fails.
func ralphBinPath() string {
	if p, err := os.Executable(); err == nil && p != "" {
		return p
	}
	return "radioactive_ralph"
}
