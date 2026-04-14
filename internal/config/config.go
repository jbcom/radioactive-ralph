// Package config loads radioactive-ralph's per-repo TOML configuration and
// produces a resolved view that the supervisor consumes.
//
// Two files live under .radioactive-ralph/ in every repo that uses Ralph:
//
//   - config.toml (committed) — declared capability biases, per-variant
//     overrides, and daemon-wide defaults.
//   - local.toml (gitignored) — operator-specific overrides that don't
//     belong in git: multiplexer preference, log verbosity, etc.
//
// The config package only parses and validates. Applying variant defaults
// and safety floors happens via Resolve() once a VariantProfile is
// available; the profile itself is defined in the variant package.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Filename constants for the per-repo config directory.
const (
	// Dir is the directory under the repo root.
	Dir = ".radioactive-ralph"

	// ConfigFile is committed.
	ConfigFile = "config.toml"

	// LocalFile is gitignored; per-operator overrides.
	LocalFile = "local.toml"

	// GitignoreFile inside Dir pins what ralph init appended so we can
	// tell apart operator edits from generated lines on ralph init --refresh.
	GitignoreFile = ".gitignore"
)

// File represents the shape of config.toml. Every section is optional so
// that a fresh `ralph init` can emit minimal files and iterate.
type File struct {
	Capabilities Capabilities           `toml:"capabilities"`
	Daemon       Daemon                 `toml:"daemon"`
	Variants     map[string]VariantFile `toml:"variants"`
}

// Capabilities declares the operator's preferred skill per bias category.
// A zero-valued string means "no preference / don't bias". The keys match
// the BiasCategory constants defined in the variant package (M3).
type Capabilities struct {
	Review         string `toml:"review"`
	SecurityReview string `toml:"security_review"`
	DocsQuery      string `toml:"docs_query"`
	Brainstorm     string `toml:"brainstorm"`
	Debugging      string `toml:"debugging"`

	// DisabledBiases lists skills the operator explicitly never wants
	// Ralph to bias toward, even when they're present in the inventory.
	// This is how operators opt out of a specific review skill in favor
	// of another.
	DisabledBiases []string `toml:"disabled_biases"`
}

// Daemon holds repo-wide defaults. Individual variants override these in
// their own [variants.<name>] section; safety floors still apply on top.
type Daemon struct {
	DefaultObjectStore      string `toml:"default_object_store"` // "reference" | "full"
	DefaultLfsMode          string `toml:"default_lfs_mode"`     // "full" | "on-demand" | "pointers-only" | "excluded"
	CopyHooks               *bool  `toml:"copy_hooks"`           // pointer so "unset" ≠ false
	AllowConcurrentVariants *bool  `toml:"allow_concurrent_variants"`
	MultiplexerPreference   string `toml:"multiplexer_preference"` // "tmux" | "screen" | "setsid"
	LogLevel                string `toml:"log_level"`              // "debug" | "info" | "warn" | "error"
}

// VariantFile is the per-variant overrides block inside config.toml.
// Any field left zero-valued falls through to the variant profile's
// hardcoded default, which falls through to Daemon, which falls through
// to project defaults. Safety floors may override any of these.
type VariantFile struct {
	Isolation      string   `toml:"isolation"`
	ObjectStore    string   `toml:"object_store"`
	SyncSource     string   `toml:"sync_source"`
	LfsMode        string   `toml:"lfs_mode"`
	ReviewBias     string   `toml:"review_bias"`
	SecurityBias   string   `toml:"security_review_bias"`
	DocsQueryBias  string   `toml:"docs_query_bias"`
	BrainstormBias string   `toml:"brainstorm_bias"`
	DebuggingBias  string   `toml:"debugging_bias"`
	SpendCapUSD    *float64 `toml:"spend_cap_usd"`
	CycleLimit     *int     `toml:"cycle_limit"`
	// Extra is a catch-all for forward-compat; unknown keys at decode time
	// are tolerated (extra=allow equivalent), but callers that want to
	// warn on unknown fields can read Unknown after Load.
}

// Local is the shape of local.toml (gitignored per-operator preferences).
// Keeping it minimal on purpose — everything else belongs in config.toml.
type Local struct {
	MultiplexerPreference string `toml:"multiplexer_preference"`
	LogLevel              string `toml:"log_level"`
}

// Errors returned by the config package.
var (
	// ErrMissingConfig is returned when the caller expects a config file
	// to exist but it doesn't. Use IsMissingConfig to check.
	ErrMissingConfig = errors.New("config: .radioactive-ralph/config.toml not found; run `ralph init` first")

	// ErrMissingLocal is returned when the caller expects a local.toml
	// and it doesn't exist (typical case: teammate cloned the repo and
	// hasn't run `ralph init --local-only` yet).
	ErrMissingLocal = errors.New("config: .radioactive-ralph/local.toml not found; run `ralph init --local-only` to create it")
)

// IsMissingConfig reports whether err indicates a missing config.toml.
func IsMissingConfig(err error) bool {
	return errors.Is(err, ErrMissingConfig)
}

// IsMissingLocal reports whether err indicates a missing local.toml.
func IsMissingLocal(err error) bool {
	return errors.Is(err, ErrMissingLocal)
}

// Load parses the per-repo config file(s) under repoRoot/.radioactive-ralph/.
// It returns ErrMissingConfig if config.toml is absent.
func Load(repoRoot string) (File, error) {
	var zero File
	path := filepath.Join(repoRoot, Dir, ConfigFile)
	data, err := os.ReadFile(path) //nolint:gosec // path is under repoRoot/.radioactive-ralph, caller-controlled
	if errors.Is(err, fs.ErrNotExist) {
		return zero, ErrMissingConfig
	}
	if err != nil {
		return zero, fmt.Errorf("config: read %s: %w", path, err)
	}
	var f File
	if _, err := toml.Decode(string(data), &f); err != nil {
		return zero, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if f.Variants == nil {
		f.Variants = make(map[string]VariantFile)
	}
	return f, nil
}

// LoadLocal parses the local.toml file under repoRoot/.radioactive-ralph/.
// Returns ErrMissingLocal if absent; callers can decide whether to treat
// that as fatal or fall through to Daemon defaults.
func LoadLocal(repoRoot string) (Local, error) {
	var zero Local
	path := filepath.Join(repoRoot, Dir, LocalFile)
	data, err := os.ReadFile(path) //nolint:gosec // path is under repoRoot/.radioactive-ralph, caller-controlled
	if errors.Is(err, fs.ErrNotExist) {
		return zero, ErrMissingLocal
	}
	if err != nil {
		return zero, fmt.Errorf("config: read %s: %w", path, err)
	}
	var l Local
	if _, err := toml.Decode(string(data), &l); err != nil {
		return zero, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return l, nil
}

// Path returns the absolute path to config.toml for repoRoot.
func Path(repoRoot string) string {
	return filepath.Join(repoRoot, Dir, ConfigFile)
}

// LocalPath returns the absolute path to local.toml for repoRoot.
func LocalPath(repoRoot string) string {
	return filepath.Join(repoRoot, Dir, LocalFile)
}
