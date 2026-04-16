package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeConfigToml(t *testing.T, repoRoot, contents string) {
	t.Helper()
	dir := filepath.Join(repoRoot, Dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(contents), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func writeLocalToml(t *testing.T, repoRoot, contents string) {
	t.Helper()
	dir := filepath.Join(repoRoot, Dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, LocalFile), []byte(contents), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	repo := t.TempDir()
	_, err := Load(repo)
	if !IsMissingConfig(err) {
		t.Fatalf("expected ErrMissingConfig, got %v", err)
	}
}

func TestLoadEmpty(t *testing.T) {
	repo := t.TempDir()
	writeConfigToml(t, repo, "")

	f, err := Load(repo)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f.Variants == nil {
		t.Error("Variants map should be initialised even when empty")
	}
	if f.Providers == nil {
		t.Error("Providers map should be initialised even when empty")
	}
}

func TestLoadFullExample(t *testing.T) {
	repo := t.TempDir()
	writeConfigToml(t, repo, `
default_provider = "codex"

[service]
default_object_store      = "reference"
default_lfs_mode          = "on-demand"
copy_hooks                = true
allow_concurrent_variants = true

[providers.codex]
type = "codex"
binary = "codex"
medium_effort = "medium"

[providers.gemini]
type = "gemini"
binary = "gemini"

[variants.green]
provider = "gemini"

[variants.red]
cycle_limit = 2

[variants.immortal]
object_store = "full"
spend_cap_usd = 25.0

[variants.old_man]
# object_store pinned by safety floor
`)
	f, err := Load(repo)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if f.Service.DefaultObjectStore != "reference" {
		t.Errorf("DefaultObjectStore = %q", f.Service.DefaultObjectStore)
	}
	if f.Service.CopyHooks == nil || !*f.Service.CopyHooks {
		t.Errorf("CopyHooks pointer should be *true")
	}
	if f.DefaultProvider != "codex" {
		t.Errorf("DefaultProvider = %q, want codex", f.DefaultProvider)
	}
	if f.Providers["codex"].Binary != "codex" {
		t.Errorf("providers.codex.binary = %q", f.Providers["codex"].Binary)
	}
	if f.Variants["green"].Provider != "gemini" {
		t.Errorf("variants.green.provider = %q", f.Variants["green"].Provider)
	}
	if got := f.Variants["red"].CycleLimit; got == nil || *got != 2 {
		t.Errorf("variants.red.cycle_limit = %v", got)
	}
	if f.Variants["immortal"].ObjectStore != "full" {
		t.Errorf("variants.immortal.object_store = %q", f.Variants["immortal"].ObjectStore)
	}
	if gotCap := f.Variants["immortal"].SpendCapUSD; gotCap == nil || *gotCap != 25.0 {
		t.Errorf("variants.immortal.spend_cap_usd = %v", gotCap)
	}
	if _, ok := f.Variants["old_man"]; !ok {
		t.Error("expected variants.old_man entry even if empty")
	}
}

func TestLoadDefaultProviderDerivedFromSingleProvider(t *testing.T) {
	repo := t.TempDir()
	writeConfigToml(t, repo, `
[providers.claude]
type = "claude"
binary = "claude"
`)
	f, err := Load(repo)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f.DefaultProvider != "claude" {
		t.Fatalf("DefaultProvider = %q, want claude", f.DefaultProvider)
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	repo := t.TempDir()
	writeConfigToml(t, repo, `this is [not valid toml`)

	_, err := Load(repo)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if IsMissingConfig(err) {
		t.Errorf("should not report missing when file exists: %v", err)
	}
}

func TestLoadLocalMissing(t *testing.T) {
	repo := t.TempDir()
	_, err := LoadLocal(repo)
	if !IsMissingLocal(err) {
		t.Fatalf("expected ErrMissingLocal, got %v", err)
	}
}

func TestLoadLocal(t *testing.T) {
	repo := t.TempDir()
	writeLocalToml(t, repo, `
provider_binary = "/opt/bin/codex"
log_level       = "debug"
`)
	l, err := LoadLocal(repo)
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if l.ProviderBinary != "/opt/bin/codex" {
		t.Errorf("ProviderBinary = %q", l.ProviderBinary)
	}
	if l.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", l.LogLevel)
	}
}

func TestPath(t *testing.T) {
	got := Path("/path/to/repo")
	want := filepath.Join("/path/to/repo", Dir, ConfigFile)
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestLocalPath(t *testing.T) {
	got := LocalPath("/path/to/repo")
	want := filepath.Join("/path/to/repo", Dir, LocalFile)
	if got != want {
		t.Errorf("LocalPath = %q, want %q", got, want)
	}
}

func TestIsMissingConfigRejectsOther(t *testing.T) {
	if IsMissingConfig(errors.New("something else")) {
		t.Error("IsMissingConfig should reject unrelated errors")
	}
	if !IsMissingConfig(ErrMissingConfig) {
		t.Error("IsMissingConfig should accept ErrMissingConfig")
	}
}
