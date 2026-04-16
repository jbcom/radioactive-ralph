package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/config"
)

func init() {
	// Pin the date so scaffolded frontmatter is reproducible across runs.
	nowUTC = func() time.Time { return time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC) }
}

func TestInitCreatesFreshConfigTree(t *testing.T) {
	repo := t.TempDir()
	res, err := Init(Options{RepoRoot: repo})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	if _, err := os.Stat(res.ConfigPath); err != nil {
		t.Errorf("config.toml missing: %v", err)
	}
	if _, err := os.Stat(res.LocalPath); err != nil {
		t.Errorf("local.toml missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.PlansPath, "index.md")); err != nil {
		t.Errorf("plans/index.md missing: %v", err)
	}
	gi, err := os.ReadFile(res.GitIgnore) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gi), ".radioactive-ralph/local.toml") {
		t.Errorf(".gitignore missing local.toml entry:\n%s", gi)
	}
}

func TestInitRefusesToClobberExistingConfig(t *testing.T) {
	repo := t.TempDir()
	if _, err := Init(Options{RepoRoot: repo}); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	_, err := Init(Options{RepoRoot: repo})
	if err == nil {
		t.Fatal("expected refusal on second Init without Force")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestInitForceOverwrites(t *testing.T) {
	repo := t.TempDir()
	if _, err := Init(Options{RepoRoot: repo}); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if _, err := Init(Options{RepoRoot: repo, Force: true}); err != nil {
		t.Errorf("force overwrite failed: %v", err)
	}
}

func TestInitRefreshPreservesRepoSettings(t *testing.T) {
	repo := t.TempDir()
	if _, err := Init(Options{RepoRoot: repo}); err != nil {
		t.Fatalf("initial Init: %v", err)
	}
	custom := `
[service]
default_object_store = "full"

default_provider = "codex"

[providers.codex]
type = "codex"
binary = "codex"

[variants.green]
provider = "gemini"
spend_cap_usd = 12.5
`
	if err := os.WriteFile(config.Path(repo), []byte(custom), 0o600); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	if _, err := Init(Options{RepoRoot: repo, Refresh: true}); err != nil {
		t.Fatalf("refresh Init: %v", err)
	}
	f, err := config.Load(repo)
	if err != nil {
		t.Fatalf("config.Load after refresh: %v", err)
	}
	if f.DefaultProvider != "codex" {
		t.Fatalf("DefaultProvider = %q, want codex", f.DefaultProvider)
	}
	if f.Service.DefaultObjectStore != "full" {
		t.Fatalf("DefaultObjectStore = %q, want full", f.Service.DefaultObjectStore)
	}
	if f.Variants["green"].Provider != "gemini" {
		t.Fatalf("variants.green.provider = %q, want gemini", f.Variants["green"].Provider)
	}
	if f.Variants["green"].SpendCapUSD == nil || *f.Variants["green"].SpendCapUSD != 12.5 {
		t.Fatalf("variants.green.spend_cap_usd = %v, want 12.5", f.Variants["green"].SpendCapUSD)
	}
}

func TestInitAppendGitIgnoreIdempotent(t *testing.T) {
	repo := t.TempDir()
	if _, err := Init(Options{RepoRoot: repo}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(repo, ".gitignore")) //nolint:gosec // test path
	if _, err := Init(Options{RepoRoot: repo, Force: true}); err != nil {
		t.Fatalf("Init 2: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(repo, ".gitignore")) //nolint:gosec // test path
	if string(before) != string(after) {
		t.Errorf(".gitignore changed between idempotent runs:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestInitPlansIndexHasFrontmatter(t *testing.T) {
	repo := t.TempDir()
	if _, err := Init(Options{RepoRoot: repo}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(repo, ".radioactive-ralph", "plans", "index.md"))
	if err != nil {
		t.Fatalf("read index.md: %v", err)
	}
	content := string(raw)
	for _, want := range []string{
		"---", "status:", "updated: 2026-04-14", "domain:",
		"variant_recommendation:", "fixit",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("index.md missing %q:\n%s", want, content)
		}
	}
}

func TestInitConfigFileLoadableByConfigPackage(t *testing.T) {
	repo := t.TempDir()
	if _, err := Init(Options{RepoRoot: repo}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	f, err := config.Load(repo)
	if err != nil {
		t.Fatalf("config.Load after Init: %v", err)
	}
	if f.DefaultProvider != "claude" {
		t.Errorf("DefaultProvider = %q, want claude", f.DefaultProvider)
	}
	for _, name := range []string{"claude", "codex", "gemini"} {
		if _, ok := f.Providers[name]; !ok {
			t.Errorf("expected provider %q to be present", name)
		}
	}
}

func TestInitErrorsWhenRepoRootMissing(t *testing.T) {
	_, err := Init(Options{RepoRoot: "/nonexistent/path/that/does/not/exist"})
	if err == nil {
		t.Fatal("expected error for missing RepoRoot")
	}
}
