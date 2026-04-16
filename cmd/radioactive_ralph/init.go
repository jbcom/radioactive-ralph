package main

import (
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/initcmd"
)

// InitCmd is `radioactive_ralph init`.
type InitCmd struct {
	RepoRoot string `help:"Repo root to initialize. Defaults to cwd." type:"path" default:""`
	Force    bool   `help:"Overwrite existing config.toml."`
	Refresh  bool   `help:"Re-write config while preserving existing repo settings."`
	Yes      bool   `help:"No-op compatibility flag; init is non-interactive."`
}

// Run executes the init subcommand.
func (c *InitCmd) Run(rc *runContext) error {
	repo, err := resolveRepoRoot(c.RepoRoot)
	if err != nil {
		return err
	}

	res, err := initcmd.Init(initcmd.Options{
		RepoRoot: repo,
		Force:    c.Force,
		Refresh:  c.Refresh,
	})
	if err != nil {
		return err
	}

	fmt.Printf("wrote %s\n", res.ConfigPath)
	fmt.Printf("wrote %s (gitignored)\n", res.LocalPath)
	fmt.Printf("scaffolded %s/index.md\n", res.PlansPath)
	fmt.Printf("updated %s\n", res.GitIgnore)
	_ = c.Yes
	_ = rc
	return nil
}
