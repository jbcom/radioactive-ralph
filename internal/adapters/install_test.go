package adapters

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestInstallReplacesBareCode127WithAbsoluteHookCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows providers are disabled")
	}

	// Exact pre-fix reproduction: the old generated command depended on PATH.
	// A fresh empty PATH cannot resolve it and the shell reports 127.
	emptyPath := t.TempDir()
	old := exec.Command("/bin/sh", "-c", "radioactive_ralph hook event --adapter claude --event PostToolUse")
	old.Env = []string{"PATH=" + emptyPath}
	err := old.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 127 {
		t.Fatalf("bare hook exit = %v, want exact code 127", err)
	}

	source := filepath.Join(t.TempDir(), "fake-ralph")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	target := filepath.Join(t.TempDir(), "bundle with spaces")
	manifest, err := Install(source, target)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if manifest.Version != BundleVersion || len(manifest.Files) != 4 {
		t.Fatalf("manifest = %+v", manifest)
	}

	configPath := filepath.Join(target, "current", "claude-hooks.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read Claude hooks: %v", err)
	}
	var config struct {
		Hooks struct {
			PostToolUse []struct {
				Hooks []struct {
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PostToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode Claude hooks: %v", err)
	}
	if len(config.Hooks.PostToolUse) != 1 || len(config.Hooks.PostToolUse[0].Hooks) != 1 {
		t.Fatalf("PostToolUse hook was not rendered: %s", raw)
	}
	command := config.Hooks.PostToolUse[0].Hooks[0].Command
	wantAbsolute := filepath.Join(target, "current", "bin", "radioactive_ralph")
	if !strings.Contains(command, wantAbsolute) || strings.HasPrefix(command, "radioactive_ralph ") {
		t.Fatalf("generated command is not absolute: %q", command)
	}
	codexRaw, err := os.ReadFile(filepath.Join(target, "current", "codex-hooks.toml"))
	if err != nil {
		t.Fatalf("read Codex hooks: %v", err)
	}
	var codexConfig map[string]any
	if _, err := toml.Decode(string(codexRaw), &codexConfig); err != nil {
		t.Fatalf("generated Codex TOML is invalid: %v\n%s", err, codexRaw)
	}
	if !strings.Contains(string(codexRaw), wantAbsolute) ||
		!strings.Contains(string(codexRaw), "[[hooks.PostToolUse]]") ||
		!strings.Contains(string(codexRaw), "[[hooks.Stop]]") {
		t.Fatalf("Codex hooks missing absolute commands/events: %s", codexRaw)
	}
	opencodeRaw, err := os.ReadFile(filepath.Join(target, "current", "opencode-plugin.js"))
	if err != nil {
		t.Fatalf("read OpenCode plugin: %v", err)
	}
	if !strings.Contains(string(opencodeRaw), wantAbsolute) ||
		!strings.Contains(string(opencodeRaw), `"tool.execute.after"`) ||
		!strings.Contains(string(opencodeRaw), `stdin: new Blob([JSON.stringify(payload)])`) ||
		!strings.Contains(string(opencodeRaw), `invoke("PostToolUse"`) ||
		strings.Contains(string(opencodeRaw), `session.idle`) ||
		strings.Contains(string(opencodeRaw), `invoke("Stop"`) {
		t.Fatalf("OpenCode plugin missing absolute commands/events: %s", opencodeRaw)
	}
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("bun is required to execute the generated OpenCode plugin smoke")
	}
	pluginPath := filepath.Join(target, "current", "opencode-plugin.js")
	script := `const plugin = await import(` + strconv.Quote(pluginPath) + `); ` +
		`const hooks = await plugin.RadioactiveRalphEnforcement(); ` +
		`if (JSON.stringify(Object.keys(hooks)) !== JSON.stringify(["tool.execute.after"])) throw new Error("unexpected hooks"); ` +
		`await hooks["tool.execute.after"]({sessionID: "smoke"});`
	cmd := exec.Command(bun, "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("OpenCode Bun.spawn Blob smoke: %v\n%s", err, out)
	}

	// Post-fix smoke under the identical sanitized PATH. The command resolves
	// through its absolute installed path and exits successfully.
	smoke := exec.Command("/bin/sh", "-c", command)
	smoke.Env = []string{"PATH=" + emptyPath}
	smoke.Stdin = strings.NewReader(`{"hook_event_name":"PostToolUse"}`)
	if out, err := smoke.CombinedOutput(); err != nil {
		t.Fatalf("absolute hook smoke: %v\n%s", err, out)
	}

	// Same binary is idempotent and preserves the exact current target.
	if _, err := Install(source, target); err != nil {
		t.Fatalf("idempotent Install: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Install(source, target)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Install: %v", err)
		}
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(target, "current"))
	if err != nil || filepath.Base(resolved) != manifest.ExecutableSHA256 {
		t.Fatalf("current release = %q, err=%v", resolved, err)
	}
	if err := os.WriteFile(filepath.Join(resolved, "claude-hooks.json"), []byte("tampered"), 0o600); err != nil {
		t.Fatalf("tamper release fixture: %v", err)
	}
	if _, err := Install(source, target); err == nil || !strings.Contains(err.Error(), "release is corrupt") {
		t.Fatalf("Install accepted a poisoned content-addressed release: %v", err)
	}
}

func TestRenderedOpenCodeProgressHookSwallowsSpawnFailure(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("bun is required to execute the generated OpenCode plugin smoke")
	}
	missingHook := filepath.Join(t.TempDir(), "missing-radioactive-ralph")
	generated, err := render(missingHook)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	pluginPath := filepath.Join(t.TempDir(), "opencode-plugin.js")
	if err := os.WriteFile(pluginPath, generated["opencode-plugin.js"], 0o600); err != nil {
		t.Fatalf("write OpenCode plugin: %v", err)
	}
	script := `const plugin = await import(` + strconv.Quote(pluginPath) + `); ` +
		`const hooks = await plugin.RadioactiveRalphEnforcement(); ` +
		`await hooks["tool.execute.after"]({sessionID: "progress-only"});`
	cmd := exec.Command(bun, "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("progress-only plugin propagated a spawn failure: %v\n%s", err, out)
	} else if len(out) != 0 {
		t.Fatalf("progress-only plugin exposed spawn failure output: %q", out)
	}
}

func TestInstallRejectsEmptyTargetWithoutMutatingWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows providers are disabled")
	}

	workDir := t.TempDir()
	t.Chdir(workDir)
	source := filepath.Join(t.TempDir(), "fake-ralph")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	before, err := os.Stat(workDir)
	if err != nil {
		t.Fatalf("stat working directory before install: %v", err)
	}

	for _, target := range []string{"", " \t\n"} {
		if _, err := Install(source, target); err == nil || !strings.Contains(err.Error(), "target directory is required") {
			t.Fatalf("Install target %q error = %v, want required-target rejection", target, err)
		}
	}

	after, err := os.Stat(workDir)
	if err != nil {
		t.Fatalf("stat working directory after install: %v", err)
	}
	if before.Mode().Perm() != after.Mode().Perm() {
		t.Fatalf("working directory mode changed from %o to %o", before.Mode().Perm(), after.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(workDir, "releases")); !os.IsNotExist(err) {
		t.Fatalf("empty target created installer state: %v", err)
	}
}

func TestInstallRejectsSymlinkedExistingReleaseEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("native Windows providers are disabled")
	}
	source := filepath.Join(t.TempDir(), "fake-ralph")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	target := filepath.Join(t.TempDir(), "bundle")
	manifest, err := Install(source, target)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	release := filepath.Join(target, "releases", manifest.ExecutableSHA256)
	entry := filepath.Join(release, "claude-hooks.json")
	original, err := os.ReadFile(entry)
	if err != nil {
		t.Fatalf("read release entry: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, original, 0o600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Remove(entry); err != nil {
		t.Fatalf("remove release entry: %v", err)
	}
	if err := os.Symlink(outside, entry); err != nil {
		t.Fatalf("plant release symlink: %v", err)
	}
	if _, err := Install(source, target); err == nil || !strings.Contains(err.Error(), "release is corrupt") {
		t.Fatalf("Install accepted symlinked release entry: %v", err)
	}
}
