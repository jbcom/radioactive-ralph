//go:build darwin || linux

package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeclaredWritePathsReachTheKernelPolicy is the wiring assertion, and it is
// the whole point of the declaration.
//
// A WritePaths entry that never reaches contain.NewPolicy is INERT: codex would
// still fail to start under containment while the config record claims the path
// is granted. That is the defect class this codebase has closed four times
// already (the contain config key, WithProviderWriteContainment, the
// declarative stream-json shape, and ralph-task `binding`) -- each looked
// complete from its own side and stopped one layer short.
//
// Asserts on the WRAPPED ARGV, which is what the kernel actually receives,
// rather than on the Request field being populated.
func TestDeclaredWritePathsReachTheKernelPolicy(t *testing.T) {
	root := t.TempDir()
	grant := t.TempDir()

	bin, args, err := applyContainment(root, []string{grant}, "/bin/sh", []string{"-c", "true"})
	if err != nil {
		t.Fatalf("applyContainment: %v", err)
	}
	if bin == "/bin/sh" {
		t.Skip("containment unavailable on this host")
	}
	joined := strings.Join(args, " ")
	resolved, _ := filepath.EvalSymlinks(grant)
	if !strings.Contains(joined, resolved) {
		t.Fatalf("declared write path %q never reached the wrapped command: %v\n"+
			"the binding records it, the policy never sees it, and the provider still "+
			"cannot start under containment", resolved, args)
	}
}

// TestNoDeclaredPathsWrapsExactlyAsBefore keeps the change from widening the
// boundary for providers that never asked. Claude declares nothing and must be
// wrapped identically to before this feature existed.
func TestNoDeclaredPathsWrapsExactlyAsBefore(t *testing.T) {
	root := t.TempDir()
	withNone, argsNone, err := applyContainment(root, nil, "/bin/sh", []string{"-c", "true"})
	if err != nil {
		t.Fatalf("applyContainment: %v", err)
	}
	if withNone == "/bin/sh" {
		t.Skip("containment unavailable on this host")
	}
	home, herr := os.UserHomeDir()
	if herr == nil && strings.Contains(strings.Join(argsNone, " "), home) {
		t.Fatalf("a binding declaring NO write paths still granted something under "+
			"the home directory: %v", argsNone)
	}
}
