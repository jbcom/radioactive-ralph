package adapters

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const openCodeRuntimePrefix = "launch-"

// OpenCodeRuntimePaths identifies one launch-private writable OpenCode world.
// It is deliberately outside the immutable content-addressed adapter release.
type OpenCodeRuntimePaths struct {
	Root      string
	Home      string
	ConfigDir string
	parent    string
}

// PrepareOpenCodeRuntime creates one private home/config pair for a single
// managed provider launch. Call Cleanup after the provider and final Stop
// decision have both completed.
func PrepareOpenCodeRuntime(bundle BundlePaths) (OpenCodeRuntimePaths, error) {
	root, err := os.MkdirTemp(bundle.OpenCodeRuntimeDir, openCodeRuntimePrefix)
	if err != nil {
		return OpenCodeRuntimePaths{}, fmt.Errorf("adapters: create OpenCode runtime: %w", err)
	}
	runtime := OpenCodeRuntimePaths{
		Root: root, Home: filepath.Join(root, "home"), ConfigDir: filepath.Join(root, "config"),
		parent: bundle.OpenCodeRuntimeDir,
	}
	for _, path := range []string{runtime.Home, runtime.ConfigDir} {
		if err := os.Mkdir(path, 0o700); err != nil {
			_ = os.RemoveAll(root)
			return OpenCodeRuntimePaths{}, fmt.Errorf("adapters: create OpenCode runtime directory: %w", err)
		}
	}
	return runtime, nil
}

// ResolveOpenCodeRuntime validates that root is one exact installer-owned
// launch directory and returns its fixed home/config children.
func ResolveOpenCodeRuntime(bundle BundlePaths, root string) (OpenCodeRuntimePaths, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return OpenCodeRuntimePaths{}, fmt.Errorf("adapters: resolve OpenCode runtime: %w", err)
	}
	if !isOpenCodeRuntimeChild(bundle.OpenCodeRuntimeDir, root) {
		return OpenCodeRuntimePaths{}, fmt.Errorf("adapters: OpenCode runtime is outside the runtime root")
	}
	runtime := OpenCodeRuntimePaths{
		Root: root, Home: filepath.Join(root, "home"), ConfigDir: filepath.Join(root, "config"),
		parent: bundle.OpenCodeRuntimeDir,
	}
	for _, path := range []string{runtime.Root, runtime.Home, runtime.ConfigDir} {
		info, statErr := os.Lstat(path) //nolint:gosec // path is constrained to one fixed runtime child
		if statErr != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			return OpenCodeRuntimePaths{}, fmt.Errorf("adapters: OpenCode runtime directory is invalid")
		}
	}
	if !safeManagedOpenCodeDirectories(runtime.Home, runtime.ConfigDir) {
		return OpenCodeRuntimePaths{}, fmt.Errorf("adapters: OpenCode runtime is not isolated")
	}
	return runtime, nil
}

func isOpenCodeRuntimeChild(parent, root string) bool {
	rel, err := filepath.Rel(parent, root)
	return err == nil && rel != "." && filepath.Dir(rel) == "." &&
		len(rel) > len(openCodeRuntimePrefix) && strings.HasPrefix(rel, openCodeRuntimePrefix)
}

// Cleanup removes only the launch-private child carried by a runtime value
// created or resolved by this package. It restores traversal on directories
// without following symlinks so provider-created mode changes cannot strand
// copied credentials. A caller-constructed value fails without deleting.
func (r OpenCodeRuntimePaths) Cleanup() error {
	if r.parent == "" || !isOpenCodeRuntimeChild(r.parent, r.Root) {
		return fmt.Errorf("adapters: invalid OpenCode runtime cleanup target")
	}
	if err := filepath.WalkDir(r.Root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o700) //nolint:gosec // exact launch-private directory; WalkDir never follows symlinks
		}
		return nil
	}); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("adapters: restore OpenCode runtime traversal: %w", err)
	}
	if err := os.RemoveAll(r.Root); err != nil {
		return fmt.Errorf("adapters: remove OpenCode runtime: %w", err)
	}
	return nil
}
