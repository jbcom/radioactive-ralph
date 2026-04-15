package initcmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// ensureRepoExists verifies the directory exists and looks like a repo.
func ensureRepoExists(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", root)
	}
	return nil
}

// buildConfigFile composes the config.File to marshal, taking care to
// carry forward any pre-existing daemon or variants sections from
// prior on Refresh.
func buildConfigFile(choices map[variant.BiasCategory]string, disabled []string,
	prior config.File,
) config.File {
	return config.File{
		Capabilities: config.Capabilities{
			Review:         choices[variant.BiasReview],
			SecurityReview: choices[variant.BiasSecurityReview],
			DocsQuery:      choices[variant.BiasDocsQuery],
			Brainstorm:     choices[variant.BiasBrainstorm],
			Debugging:      choices[variant.BiasDebugging],
			DisabledBiases: disabled,
		},
		Daemon:   prior.Daemon,
		Variants: prior.Variants,
	}
}

// writeTOML marshals a config.File and writes it with a header comment
// explaining what each section does.
func writeTOML(path string, file config.File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString(`# radioactive-ralph config.toml — committed.
# [capabilities] picks which installed skill each variant biases toward for
#                a given category. Empty value = no bias (silent skip).
# [daemon]       repo-wide defaults; safety floors override.
# [variants.X]   per-variant overrides; safety floors still override.

`)
	enc := toml.NewEncoder(&sb)
	if err := enc.Encode(file); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644) //nolint:gosec // config readable by all
}

// writeLocalTOML creates an empty local.toml with a header comment.
// It's intentionally minimal — the operator can fill in
// multiplexer_preference or log_level when they want to.
func writeLocalTOML(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	content := `# radioactive-ralph local.toml — gitignored.
# Per-operator preferences that don't belong in the committed config.
#
# Examples:
#   multiplexer_preference = "tmux"   # tmux | screen | setsid
#   log_level              = "info"   # debug | info | warn | error
`
	return os.WriteFile(path, []byte(content), 0o644) //nolint:gosec // config readable by all
}

// scaffoldPlans creates .radioactive-ralph/plans/ with a starter index.md
// containing the frontmatter every non-Fixit variant checks for.
func scaffoldPlans(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	indexPath := filepath.Join(dir, "index.md")
	if _, err := os.Stat(indexPath); err == nil {
		return nil // already exists; preserve operator content
	}
	content := `---
status: draft
updated: ` + todayISO() + `
domain: technical
variant_recommendation: fixit
---

# Plans index

This file is the plans-first discipline gate. Every variant except
` + "`fixit`" + ` refuses to run without a valid ` + "`plans/index.md`" + ` and
at least one referenced plan file.

Run ` + "`radioactive_ralph run --variant fixit --advise`" + ` to get a variant
recommendation for the current state of the codebase. Fixit will
write its recommendation to ` + "`.radioactive-ralph/plans/<topic>-advisor.md`" + `
and return without making any code changes.

## Referenced plans

_(Add bullet points referencing plan files as you create them.)_
`
	return os.WriteFile(indexPath, []byte(content), 0o644) //nolint:gosec // docs readable by all
}

// appendGitIgnore ensures .radioactive-ralph/local.toml is gitignored.
// If .gitignore exists, it appends the entry (idempotently). If it
// doesn't exist, it creates one.
func appendGitIgnore(path string) error {
	needle := ".radioactive-ralph/local.toml"
	existing, err := os.ReadFile(path) //nolint:gosec // path is repo .gitignore
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	content := string(existing)
	if strings.Contains(content, needle) {
		return nil
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += "# radioactive-ralph per-operator overrides\n" + needle + "\n"
	return os.WriteFile(path, []byte(content), 0o644) //nolint:gosec // .gitignore readable by all
}
