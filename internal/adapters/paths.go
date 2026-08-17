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

	activeTargetVersion = 1
	activeTargetFile    = "active-adapter-target.json"
	maxActiveTargetSize = 8 << 10
	maxManifestBytes    = 16 << 10
	maxExecutableBytes  = 256 << 20
)

type activeTargetSelection struct {
	Version int    `json:"version"`
	Target  string `json:"target"`
}

// BundlePaths are the verified executable and OpenCode resources in the
// atomically selected adapter release.
type BundlePaths struct {
	Target             string
	Root               string
	Executable         string
	OpenCodePlugin     string
	OpenCodeRuntimeDir string
}

// DefaultTarget returns the canonical user-level adapter installation root.
func DefaultTarget() (string, error) {
	root, err := xdg.StateRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "adapters"), nil
}

// ActivateTarget atomically records the verified non-secret bundle target used
// when RALPH_ADAPTER_ROOT is unset. Install remains side-effect-free beyond its
// requested target; the CLI calls this only after a successful installation.
func ActivateTarget(target string) error {
	bundle, err := ResolveCurrentBundle(target)
	if err != nil {
		return fmt.Errorf("adapters: verify active target: %w", err)
	}
	root, err := xdg.StateRoot()
	if err != nil {
		return fmt.Errorf("adapters: resolve active target state: %w", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("adapters: create active target state: %w", err)
	}
	selection := activeTargetSelection{Version: activeTargetVersion, Target: bundle.Target}
	body, err := json.Marshal(selection)
	if err != nil {
		return fmt.Errorf("adapters: encode active target: %w", err)
	}
	body = append(body, '\n')
	stage, err := os.CreateTemp(root, ".active-adapter-target-*")
	if err != nil {
		return fmt.Errorf("adapters: stage active target: %w", err)
	}
	stagePath := stage.Name()
	defer func() { _ = os.Remove(stagePath) }()
	if err := stage.Chmod(0o600); err != nil {
		_ = stage.Close()
		return fmt.Errorf("adapters: restrict active target: %w", err)
	}
	if _, err := stage.Write(body); err != nil {
		_ = stage.Close()
		return fmt.Errorf("adapters: write active target: %w", err)
	}
	if err := stage.Sync(); err != nil {
		_ = stage.Close()
		return fmt.Errorf("adapters: sync active target: %w", err)
	}
	if err := stage.Close(); err != nil {
		return fmt.Errorf("adapters: close active target: %w", err)
	}
	if err := os.Rename(stagePath, filepath.Join(root, activeTargetFile)); err != nil {
		return fmt.Errorf("adapters: publish active target: %w", err)
	}
	if err := syncDirectory(root); err != nil {
		return fmt.Errorf("adapters: sync active target state: %w", err)
	}
	return nil
}

// CurrentBundleFromEnvironment resolves and verifies the active release. An
// explicit non-secret environment path overrides the atomically selected CLI
// install target; neither surface captures a shell snapshot.
func CurrentBundleFromEnvironment(getenv Environment) (BundlePaths, error) {
	target := strings.TrimSpace(getenv(AdapterRootEnv))
	if target == "" {
		var err error
		target, err = selectedTarget()
		if err != nil {
			return BundlePaths{}, err
		}
	}
	return ResolveCurrentBundle(target)
}

func selectedTarget() (string, error) {
	root, err := xdg.StateRoot()
	if err != nil {
		return "", fmt.Errorf("adapters: resolve active target state: %w", err)
	}
	path := filepath.Join(root, activeTargetFile)
	raw, err := readVerifiedReleaseFile(path, maxActiveTargetSize)
	if err != nil {
		if os.IsNotExist(err) {
			target, defaultErr := DefaultTarget()
			if defaultErr != nil {
				return "", fmt.Errorf("adapters: resolve default target: %w", defaultErr)
			}
			return target, nil
		}
		return "", fmt.Errorf("adapters: read active target: %w", err)
	}
	var selection activeTargetSelection
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selection); err != nil || decoder.Decode(&struct{}{}) != io.EOF ||
		selection.Version != activeTargetVersion || strings.TrimSpace(selection.Target) == "" ||
		!filepath.IsAbs(selection.Target) {
		return "", fmt.Errorf("adapters: active target selection is invalid")
	}
	return selection.Target, nil
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
	// render does not open or resolve this path. It embeds the atomic `current`
	// selector into generated global hook fragments so a later reviewed release
	// can replace them without rewriting user config. Every file read and every
	// executable/resource returned below still comes from the already resolved,
	// content-verified releaseDir; substituting executable here would instead
	// freeze generated hooks to one old release and disagree with Install.
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

	runtimeDir := filepath.Join(resolvedTarget, "runtime")
	runtimeInfo, err := os.Lstat(runtimeDir) //nolint:gosec // fixed installer-owned child under the verified target
	if err != nil || !runtimeInfo.IsDir() || runtimeInfo.Mode().Perm() != 0o700 {
		return BundlePaths{}, fmt.Errorf("adapters: OpenCode runtime root is invalid")
	}

	return BundlePaths{
		Target:             target,
		Root:               releaseDir,
		Executable:         filepath.Join(releaseDir, "bin", "radioactive_ralph"),
		OpenCodePlugin:     filepath.Join(releaseDir, "opencode-plugin.js"),
		OpenCodeRuntimeDir: runtimeDir,
	}, nil
}
