// Package initcmd implements `radioactive_ralph init` — the per-repo setup wizard.
//
// Responsibilities:
//
//  1. Write .radioactive-ralph/config.toml (committed) and local.toml
//     (gitignored) with provider/service defaults.
//  2. Scaffold .radioactive-ralph/plans/ with a starter index.md so
//     non-Fixit variants have the plans structure they refuse to run
//     without.
//  3. Append .radioactive-ralph/local.toml to the repo's .gitignore.
//  4. Refuse to clobber an existing config unless Force is true;
//     support --refresh to preserve prior provider/service/variant
//     settings while rewriting the file layout.
package initcmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jbcom/radioactive-ralph/internal/config"
)

// Options drives Init.
type Options struct {
	// RepoRoot is the absolute path to the operator's repo. The
	// .radioactive-ralph/ tree is created directly under it.
	RepoRoot string

	// Force overwrites an existing config.toml. Without this, Init
	// refuses to clobber prior operator work.
	Force bool

	// Refresh rewrites config.toml from scratch but preserves existing
	// operator choices from any prior config.toml that loaded cleanly.
	Refresh bool
}

// Result summarizes what Init did.
type Result struct {
	ConfigPath string
	LocalPath  string
	PlansPath  string
	GitIgnore  string
}

// Init runs the wizard against Options. Returns a Result describing
// the paths it touched and the resolved choices.
func Init(opts Options) (Result, error) {
	var zero Result
	if opts.RepoRoot == "" {
		return zero, errors.New("initcmd: RepoRoot required")
	}
	abs, err := filepath.Abs(opts.RepoRoot)
	if err != nil {
		return zero, fmt.Errorf("abs(%q): %w", opts.RepoRoot, err)
	}
	opts.RepoRoot = abs

	configPath := config.Path(opts.RepoRoot)
	localPath := config.LocalPath(opts.RepoRoot)
	plansDir := filepath.Join(opts.RepoRoot, ".radioactive-ralph", "plans")
	gitignorePath := filepath.Join(opts.RepoRoot, ".gitignore")

	if err := ensureRepoExists(opts.RepoRoot); err != nil {
		return zero, err
	}

	// Refuse to clobber unless Force or Refresh.
	if !opts.Force && !opts.Refresh {
		if _, err := os.Stat(configPath); err == nil {
			return zero, fmt.Errorf("initcmd: %s already exists; pass Force or Refresh to overwrite", configPath)
		}
	}

	// Preserve prior operator choices on Refresh.
	var prior config.File
	if opts.Refresh {
		if existing, err := config.Load(opts.RepoRoot); err == nil {
			prior = existing
		}
	}

	// Compose the config.toml payload.
	file := buildConfigFile(prior)

	// Write config.toml.
	if err := writeTOML(configPath, file); err != nil {
		return zero, fmt.Errorf("write config.toml: %w", err)
	}

	// Write local.toml (empty shell; operator fills in later).
	if err := writeLocalTOML(localPath); err != nil {
		return zero, fmt.Errorf("write local.toml: %w", err)
	}

	// Scaffold plans/.
	if err := scaffoldPlans(plansDir); err != nil {
		return zero, fmt.Errorf("scaffold plans: %w", err)
	}

	// Update .gitignore.
	if err := appendGitIgnore(gitignorePath); err != nil {
		return zero, fmt.Errorf("update .gitignore: %w", err)
	}

	return Result{
		ConfigPath: configPath,
		LocalPath:  localPath,
		PlansPath:  plansDir,
		GitIgnore:  gitignorePath,
	}, nil
}
