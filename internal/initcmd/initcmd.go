// Package initcmd implements `ralph init` — the per-repo setup wizard.
//
// Responsibilities:
//
//  1. Resolve operator preferences for each capability bias category
//     (review, security review, docs query, brainstorm, debugging).
//     Single-candidate slots auto-select; multi-candidate slots defer
//     to the caller-provided Resolver (interactive prompts in the CLI,
//     scripted answers in tests).
//  2. Write .radioactive-ralph/config.toml (committed) and local.toml
//     (gitignored) with frontmatter comments naming alternatives for
//     later review.
//  3. Scaffold .radioactive-ralph/plans/ with a starter index.md so
//     non-Fixit variants have the plans structure they refuse to run
//     without.
//  4. Append .radioactive-ralph/local.toml to the repo's .gitignore.
//  5. Refuse to clobber an existing config unless Force is true;
//     support --refresh to re-discover capabilities while preserving
//     the operator's choices.
package initcmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/inventory"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// Resolver is the side-channel that asks the operator to pick between
// multiple candidate skills when a category has more than one install.
// The CLI wires it to stdin prompts; tests wire it to a deterministic
// map lookup.
//
// Called once per multi-candidate category. Returning "" marks that
// category as "no preference"; returning a value that isn't in
// candidates is treated as "disabled" (added to DisabledBiases).
type Resolver func(category variant.BiasCategory, candidates []string) (string, error)

// Options drives Init.
type Options struct {
	// RepoRoot is the absolute path to the operator's repo. The
	// .radioactive-ralph/ tree is created directly under it.
	RepoRoot string

	// Inventory is the pre-discovered capability snapshot. Callers can
	// pass inventory.Discover(...).
	Inventory inventory.Inventory

	// Resolver handles multi-candidate category questions. If nil and
	// any category has multiple candidates, Init returns an error
	// rather than silently dropping the ambiguity.
	Resolver Resolver

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
	Choices    map[variant.BiasCategory]string
	Disabled   []string
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

	// Resolve each bias category.
	choices, disabled, err := resolveChoices(opts.Inventory, opts.Resolver, prior)
	if err != nil {
		return zero, fmt.Errorf("resolve choices: %w", err)
	}

	// Compose the config.toml payload.
	file := buildConfigFile(choices, disabled, prior)

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
		Choices:    choices,
		Disabled:   disabled,
	}, nil
}

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

// resolveChoices walks each BiasCategory and picks the operator's
// preferred skill.
func resolveChoices(inv inventory.Inventory, resolver Resolver, prior config.File) (
	map[variant.BiasCategory]string, []string, error,
) {
	categories := []variant.BiasCategory{
		variant.BiasReview,
		variant.BiasSecurityReview,
		variant.BiasDocsQuery,
		variant.BiasBrainstorm,
		variant.BiasDebugging,
	}
	choices := make(map[variant.BiasCategory]string, len(categories))
	disabled := append([]string(nil), prior.Capabilities.DisabledBiases...)

	for _, cat := range categories {
		// Respect prior choice on Refresh.
		if previous := priorChoice(cat, prior); previous != "" {
			choices[cat] = previous
			continue
		}

		candidates := candidatesFor(cat, inv)
		switch len(candidates) {
		case 0:
			// Nothing installed for this category; skip.
			continue
		case 1:
			// Auto-select.
			choices[cat] = candidates[0]
		default:
			if resolver == nil {
				return nil, nil, fmt.Errorf(
					"category %q has %d candidates and no Resolver was provided: %v",
					cat, len(candidates), candidates,
				)
			}
			pick, err := resolver(cat, candidates)
			if err != nil {
				return nil, nil, err
			}
			switch {
			case pick == "":
				// No preference — leave empty, runtime will silently skip.
			case contains(candidates, pick):
				choices[cat] = pick
			default:
				// Pick wasn't in candidates — treat as explicit disable.
				disabled = append(disabled, pick)
			}
		}
	}
	sort.Strings(disabled)
	disabled = dedupe(disabled)
	return choices, disabled, nil
}

// candidatesFor finds skills whose FullName mentions the category
// name. Matches variant/prompt.go's inferSkill heuristic so init and
// runtime agree.
func candidatesFor(cat variant.BiasCategory, inv inventory.Inventory) []string {
	needle := strings.ReplaceAll(strings.ToLower(string(cat)), "_", "")
	var out []string
	for _, s := range inv.Skills {
		full := s.FullName()
		normalized := strings.ReplaceAll(strings.ToLower(full), "_", "")
		if strings.Contains(normalized, needle) {
			out = append(out, full)
		}
	}
	sort.Strings(out)
	return dedupe(out)
}

// priorChoice returns the operator's existing preference for cat from
// a prior config.File, or "" if not set.
func priorChoice(cat variant.BiasCategory, prior config.File) string {
	switch cat {
	case variant.BiasReview:
		return prior.Capabilities.Review
	case variant.BiasSecurityReview:
		return prior.Capabilities.SecurityReview
	case variant.BiasDocsQuery:
		return prior.Capabilities.DocsQuery
	case variant.BiasBrainstorm:
		return prior.Capabilities.Brainstorm
	case variant.BiasDebugging:
		return prior.Capabilities.Debugging
	default:
		return ""
	}
}

// buildConfigFile composes the config.File to marshal, taking care to
// carry forward any pre-existing daemon or variants sections from
// prior on Refresh.
func buildConfigFile(choices map[variant.BiasCategory]string, disabled []string,
	prior config.File,
) config.File {
	file := config.File{
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
	return file
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

Run ` + "`ralph run --variant fixit --advise`" + ` to get a variant
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

// contains reports whether s contains v.
func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// dedupe returns a sorted deduplicated copy of s.
func dedupe(s []string) []string {
	sort.Strings(s)
	out := s[:0]
	var prev string
	for _, v := range s {
		if v == prev {
			continue
		}
		out = append(out, v)
		prev = v
	}
	return out
}

// todayISO returns today's date in YYYY-MM-DD; broken out for testing.
var todayISO = defaultToday

func defaultToday() string {
	return nowUTC().Format("2006-01-02")
}
