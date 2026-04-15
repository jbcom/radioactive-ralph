package initcmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/inventory"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

func init() {
	// Pin the date so scaffolded frontmatter is reproducible across runs.
	nowUTC = func() time.Time { return time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC) }
}

func TestInitCreatesFreshConfigTree(t *testing.T) {
	repo := t.TempDir()
	inv := inventory.Inventory{
		Skills: []inventory.Skill{
			{Name: "review", Plugin: "coderabbit"},
		},
	}
	res, err := Init(Options{RepoRoot: repo, Inventory: inv})
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
	// Verify .gitignore entry.
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
	// Pre-seed a config.
	if _, err := Init(Options{RepoRoot: repo, Inventory: inventory.Inventory{}}); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	// Second Init without Force should refuse.
	_, err := Init(Options{RepoRoot: repo, Inventory: inventory.Inventory{}})
	if err == nil {
		t.Fatal("expected refusal on second Init without Force")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestInitForceOverwrites(t *testing.T) {
	repo := t.TempDir()
	_, err := Init(Options{RepoRoot: repo})
	if err != nil {
		t.Fatalf("first Init: %v", err)
	}
	_, err = Init(Options{RepoRoot: repo, Force: true})
	if err != nil {
		t.Errorf("force overwrite failed: %v", err)
	}
}

func TestInitAutoSelectsSingleCandidate(t *testing.T) {
	repo := t.TempDir()
	inv := inventory.Inventory{
		Skills: []inventory.Skill{{Name: "review", Plugin: "coderabbit"}},
	}
	res, err := Init(Options{RepoRoot: repo, Inventory: inv})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if res.Choices[variant.BiasReview] != "coderabbit:review" {
		t.Errorf("expected auto-select coderabbit:review, got %q",
			res.Choices[variant.BiasReview])
	}
}

func TestInitPromptsForMultipleCandidates(t *testing.T) {
	repo := t.TempDir()
	inv := inventory.Inventory{
		Skills: []inventory.Skill{
			{Name: "review", Plugin: "coderabbit"},
			{Name: "review", Plugin: "github"},
		},
	}
	called := false
	res, err := Init(Options{
		RepoRoot:  repo,
		Inventory: inv,
		Resolver: func(cat variant.BiasCategory, candidates []string) (string, error) {
			if cat != variant.BiasReview {
				return "", nil
			}
			called = true
			if len(candidates) != 2 {
				t.Errorf("expected 2 candidates for review, got %d", len(candidates))
			}
			return "github:review", nil
		},
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !called {
		t.Error("resolver was not called for multi-candidate category")
	}
	if res.Choices[variant.BiasReview] != "github:review" {
		t.Errorf("expected github:review, got %q", res.Choices[variant.BiasReview])
	}
}

func TestInitErrorsOnMultipleCandidatesWithoutResolver(t *testing.T) {
	repo := t.TempDir()
	inv := inventory.Inventory{
		Skills: []inventory.Skill{
			{Name: "review", Plugin: "a"},
			{Name: "review", Plugin: "b"},
		},
	}
	_, err := Init(Options{RepoRoot: repo, Inventory: inv})
	if err == nil {
		t.Fatal("expected error with ambiguous candidates + nil Resolver")
	}
	if !strings.Contains(err.Error(), "candidates") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestInitResolverEmptyReturnSkipsCategory(t *testing.T) {
	repo := t.TempDir()
	inv := inventory.Inventory{
		Skills: []inventory.Skill{
			{Name: "review", Plugin: "a"},
			{Name: "review", Plugin: "b"},
		},
	}
	res, err := Init(Options{
		RepoRoot:  repo,
		Inventory: inv,
		Resolver:  func(_ variant.BiasCategory, _ []string) (string, error) { return "", nil },
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if res.Choices[variant.BiasReview] != "" {
		t.Errorf("expected empty choice, got %q", res.Choices[variant.BiasReview])
	}
}

func TestInitResolverNonCandidatePickGoesToDisabled(t *testing.T) {
	repo := t.TempDir()
	inv := inventory.Inventory{
		Skills: []inventory.Skill{
			{Name: "review", Plugin: "a"},
			{Name: "review", Plugin: "b"},
		},
	}
	res, err := Init(Options{
		RepoRoot:  repo,
		Inventory: inv,
		Resolver: func(_ variant.BiasCategory, _ []string) (string, error) {
			return "a:review", nil // not in candidates (they are "a:review" and "b:review"; "a:review" IS in list, pick 'explicitly-disabled' instead)
		},
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	// "a:review" IS in candidates, so it should be the choice.
	if res.Choices[variant.BiasReview] != "a:review" {
		t.Errorf("expected a:review choice, got %q", res.Choices[variant.BiasReview])
	}

	// Now try an out-of-candidate pick.
	repo2 := t.TempDir()
	res2, err := Init(Options{
		RepoRoot:  repo2,
		Inventory: inv,
		Resolver: func(_ variant.BiasCategory, _ []string) (string, error) {
			return "explicitly-disabled:review", nil
		},
	})
	if err != nil {
		t.Fatalf("Init 2: %v", err)
	}
	if !containsStr(res2.Disabled, "explicitly-disabled:review") {
		t.Errorf("expected explicitly-disabled:review in Disabled, got %v", res2.Disabled)
	}
}

func TestInitRefreshPreservesChoices(t *testing.T) {
	repo := t.TempDir()
	inv := inventory.Inventory{
		Skills: []inventory.Skill{
			{Name: "review", Plugin: "coderabbit"},
		},
	}
	// First run seeds the choice.
	if _, err := Init(Options{RepoRoot: repo, Inventory: inv}); err != nil {
		t.Fatalf("initial: %v", err)
	}

	// Simulate new inventory with multiple review skills; Refresh must
	// preserve the existing choice rather than ask.
	invExpanded := inventory.Inventory{
		Skills: []inventory.Skill{
			{Name: "review", Plugin: "coderabbit"},
			{Name: "review", Plugin: "github"},
		},
	}
	res, err := Init(Options{
		RepoRoot:  repo,
		Inventory: invExpanded,
		Refresh:   true,
		Resolver:  func(_ variant.BiasCategory, _ []string) (string, error) { return "should-not-be-called", nil },
	})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if res.Choices[variant.BiasReview] != "coderabbit:review" {
		t.Errorf("Refresh should preserve prior choice; got %q",
			res.Choices[variant.BiasReview])
	}
}

func TestInitAppendGitIgnoreIdempotent(t *testing.T) {
	repo := t.TempDir()
	if _, err := Init(Options{RepoRoot: repo}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(repo, ".gitignore")) //nolint:gosec // test path
	// Second Init with force — must not duplicate the entry.
	if _, err := Init(Options{RepoRoot: repo, Force: true}); err != nil {
		t.Fatalf("Init 2: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(repo, ".gitignore")) //nolint:gosec // test path
	if string(before) != string(after) {
		t.Errorf(".gitignore changed between idempotent runs:\nbefore: %s\nafter:  %s",
			before, after)
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
	inv := inventory.Inventory{
		Skills: []inventory.Skill{{Name: "review", Plugin: "coderabbit"}},
	}
	if _, err := Init(Options{RepoRoot: repo, Inventory: inv}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// config.Load should succeed and see our choice.
	f, err := config.Load(repo)
	if err != nil {
		t.Fatalf("config.Load after Init: %v", err)
	}
	if f.Capabilities.Review != "coderabbit:review" {
		t.Errorf("Review = %q, want coderabbit:review", f.Capabilities.Review)
	}
}

func TestInitErrorsWhenRepoRootMissing(t *testing.T) {
	_, err := Init(Options{RepoRoot: "/nonexistent/path/that/does/not/exist"})
	if err == nil {
		t.Fatal("expected error for missing RepoRoot")
	}
}

func TestInitNoResolverAndAmbiguousErrorMatches(t *testing.T) {
	repo := t.TempDir()
	inv := inventory.Inventory{
		Skills: []inventory.Skill{
			{Name: "review", Plugin: "a"},
			{Name: "review", Plugin: "b"},
		},
	}
	var myErr *myTestError
	_, err := Init(Options{RepoRoot: repo, Inventory: inv})
	if errors.As(err, &myErr) {
		t.Errorf("errors.As matched unexpected type")
	}
	// sanity-check the error is returned
	if err == nil {
		t.Fatal("expected error")
	}
}

type myTestError struct{}

func (m myTestError) Error() string { return "" }

// containsStr is a small helper to avoid pulling in strings.Contains
// slice-style loops across tests.
func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
