//go:build !windows

package ipc

import (
	"os"
	"path/filepath"
	"testing"
)

// TestListenEndpoint0755StateDirNotRejected is the regression for the CI
// failure the socket-hardening introduced: a socket bound in a plain 0755
// directory (a natural state dir under a shared tempdir, e.g. a t.TempDir()
// on a CI runner) must NOT be rejected — only Ralph's own "rralph-<uid>"
// fallback leaf gets the strict ownership/perms check.
func TestListenEndpoint0755StateDirNotRejected(t *testing.T) {
	dir, err := os.MkdirTemp("", "ralph-natural-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	endpoint := filepath.Join(dir, "service.sock")
	l, err := listenEndpoint(endpoint)
	if err != nil {
		t.Fatalf("listenEndpoint on a 0755 non-fallback dir must succeed, got: %v", err)
	}
	_ = l.Close()
}

// TestListenEndpointFallbackDirHardened confirms Ralph's own rralph-<uid>
// fallback leaf is forced to 0700 before binding.
func TestListenEndpointFallbackDirHardened(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "rralph-*") // basename starts with rralph-
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	endpoint := filepath.Join(dir, "s.sock")
	l, err := listenEndpoint(endpoint)
	if err != nil {
		t.Fatalf("listenEndpoint on a rralph- fallback dir: %v", err)
	}
	defer func() { _ = l.Close() }()

	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("fallback dir mode = %o, want 0700 (hardened)", fi.Mode().Perm())
	}
}
