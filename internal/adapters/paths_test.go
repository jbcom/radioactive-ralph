//go:build !windows

package adapters

import (
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
	for _, exact := range []string{bundle.Executable, bundle.OpenCodePlugin} {
		if strings.Contains(exact, string(filepath.Separator)+"current"+string(filepath.Separator)) {
			t.Fatalf("verified execution path still traverses movable current link: %q", exact)
		}
	}

	if err := os.WriteFile(filepath.Join(resolved, "opencode-plugin.js"), []byte("tampered"), 0o600); err != nil {
		t.Fatalf("tamper plugin: %v", err)
	}
	if _, err := ResolveCurrentBundle(target); err == nil ||
		!strings.Contains(err.Error(), "release is corrupt") {
		t.Fatalf("tampered bundle accepted: %v", err)
	}
}

func TestResolveCurrentBundleAllowsRuntimeStateButRejectsConfigurationEntrypoints(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(bundle.OpenCodeHome, "runtime-state"),
		[]byte("isolated\n"), 0o600); err != nil {
		t.Fatalf("write managed runtime state: %v", err)
	}
	if _, err := ResolveCurrentBundle(target); err != nil {
		t.Fatalf("isolated runtime state rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle.OpenCodeConfigDir, "package.json"),
		[]byte(`{"private":true}`), 0o600); err != nil {
		t.Fatalf("write OpenCode package state: %v", err)
	}
	if _, err := ResolveCurrentBundle(target); err != nil {
		t.Fatalf("OpenCode package state rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle.OpenCodeConfigDir, "opencode.json"),
		[]byte(`{"plugin":["unreviewed"]}`), 0o600); err != nil {
		t.Fatalf("contaminate managed config: %v", err)
	}
	if _, err := ResolveCurrentBundle(target); err == nil ||
		!strings.Contains(err.Error(), "release is corrupt") {
		t.Fatalf("managed config content accepted: %v", err)
	}
}

func TestResolveCurrentBundleRejectsCompatibleHomeSkillDiscovery(t *testing.T) {
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
	if err := os.Mkdir(filepath.Join(bundle.OpenCodeHome, ".agents"), 0o700); err != nil {
		t.Fatalf("create compatible skill root: %v", err)
	}
	if _, err := ResolveCurrentBundle(target); err == nil ||
		!strings.Contains(err.Error(), "release is corrupt") {
		t.Fatalf("compatible home skill discovery accepted: %v", err)
	}
}
