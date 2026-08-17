package adapters

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/xdg"
)

const (
	// AdapterRootEnv is a non-secret deployment override for the generated
	// adapter bundle. Production defaults to the user-level Ralph state root.
	AdapterRootEnv = "RALPH_ADAPTER_ROOT"

	maxManifestBytes   = 16 << 10
	maxExecutableBytes = 256 << 20
)

// BundlePaths are the verified executable and OpenCode resources in the
// atomically selected adapter release.
type BundlePaths struct {
	Target            string
	Root              string
	Executable        string
	OpenCodePlugin    string
	OpenCodeHome      string
	OpenCodeConfigDir string
}

// DefaultTarget returns the canonical user-level adapter installation root.
func DefaultTarget() (string, error) {
	root, err := xdg.StateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "adapters"), nil
}

// CurrentBundleFromEnvironment resolves and verifies the active release. The
// environment surface is one explicit non-secret path, never a shell snapshot.
func CurrentBundleFromEnvironment(getenv Environment) (BundlePaths, error) {
	target := strings.TrimSpace(getenv(AdapterRootEnv))
	if target == "" {
		var err error
		target, err = DefaultTarget()
		if err != nil {
			return BundlePaths{}, fmt.Errorf("adapters: resolve default target: %w", err)
		}
	}
	return ResolveCurrentBundle(target)
}

// ResolveCurrentBundle verifies that current selects one exact content-
// addressed release rendered for this target. A missing, stale, symlinked, or
// tampered entry fails closed before a managed provider starts.
func ResolveCurrentBundle(target string) (BundlePaths, error) {
	if strings.TrimSpace(target) == "" {
		return BundlePaths{}, fmt.Errorf("adapters: target directory is required")
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return BundlePaths{}, fmt.Errorf("adapters: resolve target: %w", err)
	}
	current := filepath.Join(target, "current")
	releaseDir, err := filepath.EvalSymlinks(current)
	if err != nil {
		return BundlePaths{}, fmt.Errorf("adapters: resolve current release: %w", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return BundlePaths{}, fmt.Errorf("adapters: resolve target directory: %w", err)
	}
	releasesDir := filepath.Join(resolvedTarget, "releases")
	rel, err := filepath.Rel(releasesDir, releaseDir)
	if err != nil || rel == "." || strings.Contains(rel, string(filepath.Separator)) ||
		strings.HasPrefix(rel, "..") {
		return BundlePaths{}, fmt.Errorf("adapters: current release is outside the release root")
	}

	manifestRaw, err := readVerifiedReleaseFile(
		filepath.Join(releaseDir, "manifest.json"), maxManifestBytes)
	if err != nil {
		return BundlePaths{}, fmt.Errorf("adapters: verify current manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(manifestRaw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return BundlePaths{}, fmt.Errorf("adapters: current manifest is invalid")
	}
	if manifest.Version != BundleVersion || manifest.ExecutableSHA256 != rel ||
		len(manifest.ExecutableSHA256) != 64 {
		return BundlePaths{}, fmt.Errorf("adapters: current manifest does not identify its release")
	}

	executable := filepath.Join(releaseDir, "bin", "radioactive_ralph")
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > maxExecutableBytes {
		return BundlePaths{}, fmt.Errorf("adapters: current executable is invalid")
	}
	files, err := render(filepath.Join(current, "bin", "radioactive_ralph"))
	if err != nil {
		return BundlePaths{}, err
	}
	wantManifest := Manifest{Version: BundleVersion, ExecutableSHA256: rel}
	for name := range files {
		wantManifest.Files = append(wantManifest.Files, name)
	}
	wantManifest.Files = append(wantManifest.Files, "manifest.json")
	sort.Strings(wantManifest.Files)
	wantManifestRaw, err := json.MarshalIndent(wantManifest, "", "  ")
	if err != nil {
		return BundlePaths{}, fmt.Errorf("adapters: render current manifest: %w", err)
	}
	files["manifest.json"] = append(wantManifestRaw, '\n')
	if err := verifyRelease(releaseDir, rel, info.Size(), files); err != nil {
		return BundlePaths{}, err
	}

	return BundlePaths{
		Target:            target,
		Root:              releaseDir,
		Executable:        filepath.Join(releaseDir, "bin", "radioactive_ralph"),
		OpenCodePlugin:    filepath.Join(releaseDir, "opencode-plugin.js"),
		OpenCodeHome:      filepath.Join(releaseDir, "opencode-managed-home"),
		OpenCodeConfigDir: filepath.Join(releaseDir, "opencode-managed-config"),
	}, nil
}
