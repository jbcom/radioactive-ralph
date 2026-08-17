//go:build !windows

package adapters

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCurrentBundleVerifiesExactRelease(t *testing.T) {
	source := filepath.Join(t.TempDir(), "radioactive_ralph")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write source: %v", err)
	}
	target := filepath.Join(t.TempDir(), "adapters")
	manifest, err := Install(source, target)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	bundle, err := ResolveCurrentBundle(target)
	if err != nil {
		t.Fatalf("ResolveCurrentBundle: %v", err)
	}
	if bundle.Target != target || !filepath.IsAbs(bundle.Executable) ||
		!strings.Contains(bundle.OpenCodePlugin, "opencode-plugin.js") {
		t.Fatalf("bundle paths = %+v", bundle)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(target, "current"))
	if err != nil || bundle.Root != resolved || filepath.Base(bundle.Root) != manifest.ExecutableSHA256 {
		t.Fatalf("exact release = bundle:%q current:%q err=%v", bundle.Root, resolved, err)
	}
	for _, exact := range []string{
		bundle.Executable, bundle.OpenCodePlugin, bundle.OpenCodeRuntimeDir,
	} {
		if strings.Contains(exact, string(filepath.Separator)+"current"+string(filepath.Separator)) {
			t.Fatalf("verified execution path still traverses movable current link: %q", exact)
		}
	}
	claudeHooks, err := os.ReadFile(filepath.Join(bundle.Root, "claude-hooks.json")) //nolint:gosec // test-owned exact release
	if err != nil {
		t.Fatalf("read generated hook fragment: %v", err)
	}
	currentExecutable := filepath.Join(target, "current", "bin", "radioactive_ralph")
	if !bytes.Contains(claudeHooks, []byte(currentExecutable)) {
		t.Fatalf("generated hooks do not carry atomic current selector %q", currentExecutable)
	}

	if err := os.WriteFile(filepath.Join(resolved, "opencode-plugin.js"), []byte("tampered"), 0o600); err != nil {
		t.Fatalf("tamper plugin: %v", err)
	}
	if _, err := ResolveCurrentBundle(target); err == nil ||
		!strings.Contains(err.Error(), "release is corrupt") {
		t.Fatalf("tampered bundle accepted: %v", err)
	}
}

func TestOpenCodeRuntimeAllowsStateButRejectsConfigurationEntrypoints(t *testing.T) {
	source := filepath.Join(t.TempDir(), "radioactive_ralph")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write source: %v", err)
	}
	target := filepath.Join(t.TempDir(), "adapters")
	if _, err := Install(source, target); err != nil {
		t.Fatalf("Install: %v", err)
	}
	bundle, err := ResolveCurrentBundle(target)
	if err != nil {
		t.Fatalf("ResolveCurrentBundle: %v", err)
	}
	runtimePaths, err := PrepareOpenCodeRuntime(bundle)
	if err != nil {
		t.Fatalf("PrepareOpenCodeRuntime: %v", err)
	}
	defer runtimePaths.Cleanup()
	if err := os.WriteFile(filepath.Join(runtimePaths.Home, "runtime-state"),
		[]byte("isolated\n"), 0o600); err != nil {
		t.Fatalf("write managed runtime state: %v", err)
	}
	if _, err := ResolveOpenCodeRuntime(bundle, runtimePaths.Root); err != nil {
		t.Fatalf("isolated runtime state rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimePaths.ConfigDir, "package.json"),
		[]byte(`{"private":true}`), 0o600); err != nil {
		t.Fatalf("write OpenCode package state: %v", err)
	}
	if _, err := ResolveOpenCodeRuntime(bundle, runtimePaths.Root); err != nil {
		t.Fatalf("OpenCode package state rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimePaths.ConfigDir, "opencode.json"),
		[]byte(`{"plugin":["unreviewed"]}`), 0o600); err != nil {
		t.Fatalf("contaminate managed config: %v", err)
	}
	if _, err := ResolveOpenCodeRuntime(bundle, runtimePaths.Root); err == nil ||
		!strings.Contains(err.Error(), "not isolated") {
		t.Fatalf("managed config content accepted: %v", err)
	}
}

func TestOpenCodeRuntimeIsUniqueCleanableAndRejectsCompatibleHomeSkillDiscovery(t *testing.T) {
	source := filepath.Join(t.TempDir(), "radioactive_ralph")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write source: %v", err)
	}
	target := filepath.Join(t.TempDir(), "adapters")
	if _, err := Install(source, target); err != nil {
		t.Fatalf("Install: %v", err)
	}
	bundle, err := ResolveCurrentBundle(target)
	if err != nil {
		t.Fatalf("ResolveCurrentBundle: %v", err)
	}
	first, err := PrepareOpenCodeRuntime(bundle)
	if err != nil {
		t.Fatalf("prepare first OpenCode runtime: %v", err)
	}
	second, err := PrepareOpenCodeRuntime(bundle)
	if err != nil {
		first.Cleanup()
		t.Fatalf("prepare second OpenCode runtime: %v", err)
	}
	defer second.Cleanup()
	if first.Root == second.Root {
		t.Fatalf("concurrent OpenCode runtimes share root %q", first.Root)
	}
	if err := os.Mkdir(filepath.Join(first.Home, ".agents"), 0o700); err != nil {
		t.Fatalf("create compatible skill root: %v", err)
	}
	if _, err := ResolveOpenCodeRuntime(bundle, first.Root); err == nil ||
		!strings.Contains(err.Error(), "not isolated") {
		t.Fatalf("compatible home skill discovery accepted: %v", err)
	}
	first.Cleanup()
	if _, err := os.Stat(first.Root); !os.IsNotExist(err) {
		t.Fatalf("OpenCode runtime cleanup left root: %v", err)
	}
}

func TestCallerConstructedOpenCodeRuntimeCannotDeleteArbitraryRoot(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "keep")
	if err := os.WriteFile(marker, []byte("keep\n"), 0o600); err != nil {
		t.Fatalf("write cleanup marker: %v", err)
	}
	OpenCodeRuntimePaths{Root: root}.Cleanup()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("caller-constructed runtime deleted arbitrary root: %v", err)
	}
}
