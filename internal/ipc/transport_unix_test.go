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

// TestListenEndpointFallbackDirFreshIsSafe confirms a freshly-created
// rralph-<uid> fallback leaf (MkdirAll makes it 0700) binds fine and stays
// 0700.
func TestListenEndpointFallbackDirFreshIsSafe(t *testing.T) {
	// Use a nested rralph-<n>/leaf so MkdirAll creates the leaf 0700 itself.
	base, err := os.MkdirTemp(os.TempDir(), "rralphbase-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	dir := filepath.Join(base, "rralph-1")

	endpoint := filepath.Join(dir, "s.sock")
	l, err := listenEndpoint(endpoint)
	if err != nil {
		t.Fatalf("listenEndpoint on a fresh rralph- fallback dir: %v", err)
	}
	defer func() { _ = l.Close() }()

	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("fallback dir mode = %o, want 0700", fi.Mode().Perm())
	}
}

// TestListenEndpointFallbackDirRejectsPreexistingLoose is the regression for
// the third-pass finding: a rralph- fallback dir that ALREADY exists with
// loose (group/world) perms — the shape an attacker would pre-create — must
// be REFUSED (verify-before-mutate), NOT silently chmod'd. Silently fixing it
// is the vulnerability; a path-based chmod would also follow a symlink.
func TestListenEndpointFallbackDirRejectsPreexistingLoose(t *testing.T) {
	dir, err := os.MkdirTemp(os.TempDir(), "rralph-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	endpoint := filepath.Join(dir, "s.sock")
	if _, err := listenEndpoint(endpoint); err == nil {
		t.Fatal("listenEndpoint accepted a pre-existing 0755 fallback dir; must refuse (verify-before-mutate)")
	}
}

// TestListenEndpointFallbackDirRejectsSymlink is the core third-pass
// security regression: a rralph- fallback path that is a SYMLINK must be
// refused WITHOUT any chmod ever touching the target, closing the
// victim-privileged chmod primitive.
func TestListenEndpointFallbackDirRejectsSymlink(t *testing.T) {
	base, err := os.MkdirTemp(os.TempDir(), "rralphsym-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })

	// A victim-owned target dir at 0755 that must stay 0755 after the attempt.
	target := filepath.Join(base, "victim-target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	// The attacker-pre-created symlink named like Ralph's fallback leaf.
	link := filepath.Join(os.TempDir(), "rralph-symtest-"+filepath.Base(base))
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })

	endpoint := filepath.Join(link, "s.sock")
	if _, err := listenEndpoint(endpoint); err == nil {
		t.Fatal("listenEndpoint bound through a symlink fallback dir; must refuse")
	}
	// The target's mode must be UNCHANGED — no victim-privileged chmod happened.
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if fi.Mode().Perm() != 0o755 {
		t.Errorf("symlink target mode = %o, want 0755 unchanged (chmod must not have followed the link)", fi.Mode().Perm())
	}
}
