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
}

func TestLoadFullExample(t *testing.T) {
	repo := t.TempDir()
	writeConfigToml(t, repo, `
[capabilities]
review          = "coderabbit:review"
security_review = "sec-context-depth"
docs_query      = "plugin:context7:context7"
disabled_biases = ["code-review:code-review"]

[daemon]
default_object_store      = "reference"
default_lfs_mode          = "on-demand"
copy_hooks                = true
allow_concurrent_variants = true

[variants.green]

[variants.red]
review_bias = "coderabbit:review"

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

	if f.Capabilities.Review != "coderabbit:review" {
		t.Errorf("review = %q, want coderabbit:review", f.Capabilities.Review)
	}
	if len(f.Capabilities.DisabledBiases) != 1 || f.Capabilities.DisabledBiases[0] != "code-review:code-review" {
		t.Errorf("DisabledBiases = %v", f.Capabilities.DisabledBiases)
	}
	if f.Daemon.DefaultObjectStore != "reference" {
		t.Errorf("DefaultObjectStore = %q", f.Daemon.DefaultObjectStore)
	}
	if f.Daemon.CopyHooks == nil || !*f.Daemon.CopyHooks {
		t.Errorf("CopyHooks pointer should be *true")
	}

	if _, ok := f.Variants["green"]; !ok {
		t.Error("expected variants.green entry")
	}
	if f.Variants["red"].ReviewBias != "coderabbit:review" {
		t.Errorf("variants.red.review_bias = %q", f.Variants["red"].ReviewBias)
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
multiplexer_preference = "tmux"
log_level              = "debug"
`)
	l, err := LoadLocal(repo)
	if err != nil {
		t.Fatalf("LoadLocal: %v", err)
	}
	if l.MultiplexerPreference != "tmux" {
		t.Errorf("MultiplexerPreference = %q", l.MultiplexerPreference)
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
