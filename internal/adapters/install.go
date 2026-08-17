// Package adapters renders and installs the provider-specific edge of Ralph's
// canonical enforcement policy. Generated hooks normalize provider events and
// call one absolute Ralph binary; they do not contain policy logic.
package adapters

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// BundleVersion is the generated adapter artifact contract version.
const BundleVersion = 1

// Manifest identifies one content-addressed generated adapter release.
type Manifest struct {
	Version          int      `json:"version"`
	ExecutableSHA256 string   `json:"executable_sha256"`
	Files            []string `json:"files"`
}

// Install builds a content-addressed release beside target/current, then
// atomically switches the current symlink. No live provider config is mutated;
// deployment can merge the rendered fragments after review and feature probes.
func Install(sourceExecutable, target string) (Manifest, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return Manifest{}, fmt.Errorf("adapters: provider hook installation is unsupported on %s; use macOS, Linux, or WSL", runtime.GOOS)
	}
	if strings.TrimSpace(target) == "" {
		return Manifest{}, fmt.Errorf("adapters: target directory is required")
	}
	sourceExecutable, err := filepath.Abs(sourceExecutable)
	if err != nil {
		return Manifest{}, fmt.Errorf("adapters: resolve executable: %w", err)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return Manifest{}, fmt.Errorf("adapters: resolve target: %w", err)
	}
	raw, err := os.ReadFile(sourceExecutable) //nolint:gosec // operator-selected local executable
	if err != nil {
		return Manifest{}, fmt.Errorf("adapters: read executable: %w", err)
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	releaseName := digest
	releasesDir := filepath.Join(target, "releases")
	releaseDir := filepath.Join(releasesDir, releaseName)
	currentExecutable := filepath.Join(target, "current", "bin", "radioactive_ralph")

	files, err := render(currentExecutable)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{Version: BundleVersion, ExecutableSHA256: digest}
	for name := range files {
		manifest.Files = append(manifest.Files, name)
	}
	manifest.Files = append(manifest.Files, "manifest.json")
	sort.Strings(manifest.Files)
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, fmt.Errorf("adapters: marshal manifest: %w", err)
	}
	files["manifest.json"] = append(manifestJSON, '\n')

	if err := os.MkdirAll(releasesDir, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("adapters: create releases: %w", err)
	}
	if err := os.Chmod(target, 0o700); err != nil { //nolint:gosec // directories require execute permission to remain traversable
		return Manifest{}, fmt.Errorf("adapters: restrict target: %w", err)
	}
	if err := os.Chmod(releasesDir, 0o700); err != nil { //nolint:gosec // directories require execute permission to remain traversable
		return Manifest{}, fmt.Errorf("adapters: restrict releases: %w", err)
	}
	if _, err := os.Stat(releaseDir); os.IsNotExist(err) { //nolint:gosec // releaseDir is intentionally below the operator-selected install root
		stage, err := os.MkdirTemp(releasesDir, ".install-*")
		if err != nil {
			return Manifest{}, fmt.Errorf("adapters: create staging release: %w", err)
		}
		defer func() { _ = os.RemoveAll(stage) }()
		if err := writeRelease(stage, raw, files); err != nil {
			return Manifest{}, err
		}
		if err := os.Rename(stage, releaseDir); err != nil { //nolint:gosec // both paths are installer-owned children of releasesDir
			// A concurrent identical installer may have published the same
			// content-addressed release first. Accept only an exact byte match.
			if verifyErr := verifyRelease(releaseDir, digest, int64(len(raw)), files); verifyErr != nil {
				return Manifest{}, fmt.Errorf("adapters: publish release: %w", err)
			}
		}
	} else if err != nil {
		return Manifest{}, fmt.Errorf("adapters: inspect release: %w", err)
	} else if err := verifyRelease(releaseDir, digest, int64(len(raw)), files); err != nil {
		return Manifest{}, err
	}

	if err := switchCurrent(target, releaseDir); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func writeRelease(stage string, executable []byte, files map[string][]byte) error {
	binDir := filepath.Join(stage, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return fmt.Errorf("adapters: create bin directory: %w", err)
	}
	for _, name := range []string{"opencode-managed-home", "opencode-managed-config"} {
		if err := os.Mkdir(filepath.Join(stage, name), 0o700); err != nil {
			return fmt.Errorf("adapters: create managed OpenCode directory: %w", err)
		}
	}
	if err := writeSynced(filepath.Join(binDir, "radioactive_ralph"), executable, 0o700); err != nil {
		return err
	}
	if err := syncDirectory(binDir); err != nil {
		return err
	}
	for name, body := range files {
		if err := writeSynced(filepath.Join(stage, name), body, 0o600); err != nil {
			return err
		}
	}
	return syncDirectory(stage)
}

func writeSynced(path string, body []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) //nolint:gosec // caller passes only finite installer-owned paths
	if err != nil {
		return fmt.Errorf("adapters: create %s: %w", filepath.Base(path), err)
	}
	if _, err := io.Copy(f, bytes.NewReader(body)); err != nil {
		_ = f.Close()
		return fmt.Errorf("adapters: write %s: %w", filepath.Base(path), err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("adapters: sync %s: %w", filepath.Base(path), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("adapters: close %s: %w", filepath.Base(path), err)
	}
	return nil
}

func verifyRelease(releaseDir, digest string, executableSize int64, files map[string][]byte) error {
	info, err := os.Lstat(releaseDir) //nolint:gosec // releaseDir is installer-owned and must itself be a real directory
	if err != nil {
		return fmt.Errorf("adapters: inspect existing release: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("adapters: existing content-addressed release is corrupt")
	}
	executable, err := readVerifiedReleaseFile(
		filepath.Join(releaseDir, "bin", "radioactive_ralph"), executableSize+1)
	if err != nil {
		return fmt.Errorf("adapters: verify existing executable: %w", err)
	}
	sum := sha256.Sum256(executable)
	if hex.EncodeToString(sum[:]) != digest {
		return fmt.Errorf("adapters: existing content-addressed release is corrupt")
	}
	for _, name := range []string{"opencode-managed-home", "opencode-managed-config"} {
		dir := filepath.Join(releaseDir, name)
		entry, err := os.Lstat(dir) //nolint:gosec // fixed installer-owned child under the operator-selected release root
		if err != nil || !entry.IsDir() || entry.Mode().Perm() != 0o700 {
			return fmt.Errorf("adapters: existing content-addressed release is corrupt")
		}
	}
	if !safeManagedOpenCodeDirectories(
		filepath.Join(releaseDir, "opencode-managed-home"),
		filepath.Join(releaseDir, "opencode-managed-config"),
	) {
		return fmt.Errorf("adapters: existing content-addressed release is corrupt")
	}
	for name, want := range files {
		got, err := readVerifiedReleaseFile(filepath.Join(releaseDir, name), int64(len(want))+1)
		if err != nil || !bytes.Equal(got, want) {
			return fmt.Errorf("adapters: existing content-addressed release is corrupt")
		}
	}
	return nil
}

func readVerifiedReleaseFile(path string, limit int64) ([]byte, error) {
	file, err := openReleaseFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("release entry is not a regular file")
	}
	reader := io.Reader(file)
	if limit >= 0 {
		reader = io.LimitReader(file, limit)
	}
	return io.ReadAll(reader)
}

func reserveName(target string) (string, error) {
	marker, err := os.CreateTemp(target, ".current-*")
	if err != nil {
		return "", fmt.Errorf("adapters: reserve current link: %w", err)
	}
	tmp := marker.Name()
	if err := marker.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("adapters: close current-link reservation: %w", err)
	}
	if err := os.Remove(tmp); err != nil {
		return "", fmt.Errorf("adapters: remove current-link reservation: %w", err)
	}
	return tmp, nil
}

func switchCurrent(target, releaseDir string) error {
	var tmp string
	linked := false
	for attempt := 0; attempt < 8; attempt++ {
		reserved, err := reserveName(target)
		if err != nil {
			return err
		}
		tmp = reserved
		err = os.Symlink(releaseDir, tmp)
		if err == nil {
			linked = true
			break
		}
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return fmt.Errorf("adapters: create current link: %w", err)
	}
	if !linked {
		return fmt.Errorf("adapters: create current link after retries: %w", fs.ErrExist)
	}
	if err := os.Rename(tmp, filepath.Join(target, "current")); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("adapters: switch current release: %w", err)
	}
	return syncDirectory(target)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path) //nolint:gosec // internal installer-owned path
	if err != nil {
		return fmt.Errorf("adapters: open directory for sync: %w", err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("adapters: sync directory: %w", err)
	}
	return nil
}

func render(executable string) (map[string][]byte, error) {
	if !filepath.IsAbs(executable) {
		return nil, fmt.Errorf("adapters: generated hook executable must be absolute")
	}
	command := func(adapter, event string) string {
		return shellQuote(executable) + " hook event --adapter " + adapter + " --event " + event
	}
	claude := map[string]any{"hooks": map[string]any{
		"PostToolUse": []any{map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": command("claude", "PostToolUse"), "timeout": 30,
		}}}},
		"Stop": []any{map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": command("claude", "Stop"), "timeout": 600,
		}}}},
	}}
	claudeJSON, err := json.MarshalIndent(claude, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("adapters: render Claude hooks: %w", err)
	}
	codex := fmt.Sprintf(`[hooks]

[[hooks.PostToolUse]]
[[hooks.PostToolUse.hooks]]
type = "command"
command = %s
timeout = 30

[[hooks.Stop]]
[[hooks.Stop.hooks]]
type = "command"
command = %s
timeout = 600
`, strconv.Quote(command("codex", "PostToolUse")), strconv.Quote(command("codex", "Stop")))
	opencode := fmt.Sprintf(`const hook = %s;
const invoke = async (event, payload) => {
  const child = Bun.spawn([hook, "hook", "event", "--adapter", "opencode", "--event", event], {
    stdin: new Blob([JSON.stringify(payload)]), stdout: "pipe", stderr: "ignore", env: process.env,
  });
  await Promise.all([new Response(child.stdout).text(), child.exited]);
};
export const RadioactiveRalphEnforcement = async () => ({
  "tool.execute.after": async (input) => {
    await invoke("PostToolUse", { hook_event_name: "PostToolUse", session_id: input.sessionID });
  },
});
`, strconv.Quote(executable))
	return map[string][]byte{
		"claude-hooks.json":  append(claudeJSON, '\n'),
		"codex-hooks.toml":   []byte(codex),
		"opencode-plugin.js": []byte(opencode),
	}, nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
