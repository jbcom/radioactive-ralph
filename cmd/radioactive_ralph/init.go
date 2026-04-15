package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/initcmd"
	"github.com/jbcom/radioactive-ralph/internal/inventory"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// InitCmd is `radioactive_ralph init`.
type InitCmd struct {
	RepoRoot string `help:"Repo root to initialize. Defaults to cwd." type:"path" default:""`
	Force    bool   `help:"Overwrite existing config.toml."`
	Refresh  bool   `help:"Re-discover capabilities while preserving existing operator choices."`
	Yes      bool   `help:"Skip interactive prompts; auto-select first candidate for multi-candidate categories."`
}

// Run executes the init subcommand.
func (c *InitCmd) Run(rc *runContext) error {
	repo := c.RepoRoot
	if repo == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("cwd: %w", err)
		}
		repo = cwd
	}

	inv, errs := inventory.Discover(inventory.Options{})
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "ralph init: %d inventory warning(s):\n", len(errs))
		for _, err := range errs {
			fmt.Fprintf(os.Stderr, "  - %v\n", err)
		}
	}

	var resolver initcmd.Resolver
	if c.Yes {
		resolver = func(_ variant.BiasCategory, candidates []string) (string, error) {
			if len(candidates) == 0 {
				return "", nil
			}
			return candidates[0], nil
		}
	} else {
		resolver = stdinResolver(os.Stdin)
	}

	res, err := initcmd.Init(initcmd.Options{
		RepoRoot:  repo,
		Inventory: inv,
		Resolver:  resolver,
		Force:     c.Force,
		Refresh:   c.Refresh,
	})
	if err != nil {
		return err
	}

	fmt.Printf("wrote %s\n", res.ConfigPath)
	fmt.Printf("wrote %s (gitignored)\n", res.LocalPath)
	fmt.Printf("scaffolded %s/index.md\n", res.PlansPath)
	fmt.Printf("updated %s\n", res.GitIgnore)
	if len(res.Choices) > 0 {
		fmt.Println("\nResolved bias preferences:")
		for cat, skill := range res.Choices {
			fmt.Printf("  %s → %s\n", cat, skill)
		}
	}
	if len(res.Disabled) > 0 {
		fmt.Println("\nDisabled:")
		for _, d := range res.Disabled {
			fmt.Printf("  %s\n", d)
		}
	}
	_ = rc
	return nil
}

// stdinResolver is the interactive prompt used when operator runs
// init without --yes. Presents each candidate with a 1-based index;
// operator types a number, an empty line for "no preference", or a
// verbatim string to disable it.
func stdinResolver(in *os.File) initcmd.Resolver {
	reader := bufio.NewReader(in)
	return func(cat variant.BiasCategory, candidates []string) (string, error) {
		fmt.Fprintf(os.Stderr, "\nCategory %q has %d candidates:\n", cat, len(candidates))
		for i, c := range candidates {
			fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, c)
		}
		fmt.Fprintf(os.Stderr, "Pick a number (1-%d), empty line for no preference, or a string to disable: ",
			len(candidates))
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return "", nil
		}
		if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(candidates) {
			return candidates[n-1], nil
		}
		return line, nil
	}
}
