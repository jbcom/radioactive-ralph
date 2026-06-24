// Package config loads radioactive-ralph's per-repo TOML configuration and
// produces a resolved view that the runtime consumes.
//
// Two files live under .radioactive-ralph/ in every repo that uses Ralph:
//
//   - config.toml (committed) — provider bindings, per-variant
//     overrides, and service-wide defaults.
//   - local.toml (gitignored) — operator-specific overrides that don't
//     belong in git: local binary overrides, log verbosity, etc.
//
// The config package only parses and validates. Applying variant defaults
// and provider bindings happens at runtime boot.
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
// that a fresh `radioactive_ralph init` can emit minimal files and iterate.
type File struct {
	Service         Service                 `toml:"service"`
	DefaultProvider string                  `toml:"default_provider"`
	Providers       map[string]ProviderFile `toml:"providers"`
	Variants        map[string]VariantFile  `toml:"variants"`
}

// Service holds repo-wide defaults. Individual variants override these in
// their own [variants.<name>] section; safety floors still apply on top.
type Service struct {
	DefaultObjectStore      string `toml:"default_object_store"` // "reference" | "full"
	DefaultLfsMode          string `toml:"default_lfs_mode"`     // "full" | "on-demand" | "pointers-only" | "excluded"
	CopyHooks               *bool  `toml:"copy_hooks"`           // pointer so "unset" ≠ false
	AllowConcurrentVariants *bool  `toml:"allow_concurrent_variants"`
	LogLevel                string `toml:"log_level"` // "debug" | "info" | "warn" | "error"
}

// ProviderFile declares how one named provider is invoked. The shape
// is intentionally generic.
type ProviderFile struct {
	Type                  string   `toml:"type"`
	Bin                   string   `toml:"bin"`
	Binary                string   `toml:"binary"`
	Args                  []string `toml:"args"`
	OutputFile            string   `toml:"output_file"`
	TurnTimeout           string   `toml:"turn_timeout"`
	MaxRetries            int      `toml:"max_retries"`
	SessionIDRegex        string   `toml:"session_id_regex"`
	HaikuModel            string   `toml:"haiku_model"`
	SonnetModel           string   `toml:"sonnet_model"`
	OpusModel             string   `toml:"opus_model"`
	LowEffort             string   `toml:"low_effort"`
	MediumEffort          string   `toml:"medium_effort"`
	HighEffort            string   `toml:"high_effort"`
	MaxEffort             string   `toml:"max_effort"`
	SupportsResume        *bool    `toml:"supports_resume"`
	UseAppendSystemPrompt *bool    `toml:"use_append_system_prompt"`
}

// VariantFile is the per-variant overrides block inside config.toml.
// Any field left zero-valued falls through to the variant profile's
// hardcoded default, which falls through to Service, which falls through
// to project defaults. Safety floors may override any of these.
type VariantFile struct {
	Isolation   string   `toml:"isolation"`
	ObjectStore string   `toml:"object_store"`
	SyncSource  string   `toml:"sync_source"`
	LfsMode     string   `toml:"lfs_mode"`
	Provider    string   `toml:"provider"`
	SpendCapUSD *float64 `toml:"spend_cap_usd"`
	CycleLimit  *int     `toml:"cycle_limit"`

	// Fixit-specific advisor knobs. Only meaningful in
	// [variants.fixit]. CLI flags take precedence; these are the
	// defaults when no flag is passed.
	MaxRefinementIterations *int   `toml:"max_refinement_iterations"`
	MinConfidenceThreshold  *int   `toml:"min_confidence_threshold"`
	PlanModel               string `toml:"plan_model"`
	PlanEffort              string `toml:"plan_effort"`

	// Extra is a catch-all for forward-compat; unknown keys at decode time
	// are tolerated (extra=allow equivalent), but callers that want to
	// warn on unknown fields can read Unknown after Load.
}

// Local is the shape of local.toml (gitignored per-operator preferences).
// Keeping it minimal on purpose — everything else belongs in config.toml.
type Local struct {
	LogLevel       string `toml:"log_level"`
	ProviderBinary string `toml:"provider_binary"`
}

// Errors returned by the config package.
var (
	// ErrMissingConfig is returned when the caller expects a config file
	// to exist but it doesn't. Use IsMissingConfig to check.
	ErrMissingConfig = errors.New("config: .radioactive-ralph/config.toml not found; run `radioactive_ralph init` first")

	// ErrMissingLocal is returned when the caller expects a local.toml
	// and it doesn't exist (typical case: teammate cloned the repo and
	// hasn't run `radioactive_ralph init --local-only` yet).
	ErrMissingLocal = errors.New("config: .radioactive-ralph/local.toml not found; run `radioactive_ralph init --local-only` to create it")
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
	var raw struct {
		Service         Service                 `toml:"service"`
		DefaultProvider string                  `toml:"default_provider"`
		Providers       map[string]ProviderFile `toml:"providers"`
		Variants        map[string]VariantFile  `toml:"variants"`
	}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return zero, fmt.Errorf("config: parse %s: %w", path, err)
	}
	f := File{
		Service:         raw.Service,
		DefaultProvider: raw.DefaultProvider,
		Providers:       raw.Providers,
		Variants:        raw.Variants,
	}
	if f.Variants == nil {
		f.Variants = make(map[string]VariantFile)
	}
	if f.Providers == nil {
		f.Providers = make(map[string]ProviderFile)
	}
	if f.DefaultProvider == "" && len(f.Providers) == 1 {
		for name := range f.Providers {
			f.DefaultProvider = name
		}
	}
	for name, provider := range f.Providers {
		if provider.Binary == "" && provider.Bin != "" {
			provider.Binary = provider.Bin
			f.Providers[name] = provider
		}
	}
	return f, nil
}

// LoadLocal parses the local.toml file under repoRoot/.radioactive-ralph/.
// Returns ErrMissingLocal if absent; callers can decide whether to treat
// that as fatal or fall through to committed service defaults.
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

// DefaultClaudeProvider returns the built-in provider binding that uses
// the local `claude` CLI as the execution backend.
func DefaultClaudeProvider() ProviderFile {
	useAppend := true
	supportsResume := true
	return ProviderFile{
		Type:                  "claude",
		Binary:                "claude",
		HaikuModel:            "haiku",
		SonnetModel:           "sonnet",
		OpusModel:             "opus",
		LowEffort:             "low",
		MediumEffort:          "medium",
		HighEffort:            "high",
		MaxEffort:             "max",
		SupportsResume:        &supportsResume,
		UseAppendSystemPrompt: &useAppend,
	}
}

// DefaultCodexProvider returns the built-in provider binding that uses the
// local `codex` CLI as the execution backend.
func DefaultCodexProvider() ProviderFile {
	supportsResume := false
	return ProviderFile{
		Type:           "codex",
		Binary:         "codex",
		HaikuModel:     "gpt-5.4-mini",
		SonnetModel:    "gpt-5.4",
		OpusModel:      "gpt-5.4",
		LowEffort:      "low",
		MediumEffort:   "medium",
		HighEffort:     "high",
		MaxEffort:      "high",
		SupportsResume: &supportsResume,
	}
}

// DefaultGeminiProvider returns the built-in provider binding that uses the
// local `gemini` CLI as the execution backend.
func DefaultGeminiProvider() ProviderFile {
	supportsResume := false
	return ProviderFile{
		Type:           "gemini",
		Binary:         "gemini",
		LowEffort:      "low",
		MediumEffort:   "medium",
		HighEffort:     "high",
		MaxEffort:      "high",
		SupportsResume: &supportsResume,
	}
}

// Path returns the absolute path to config.toml for repoRoot.
func Path(repoRoot string) string {
	return filepath.Join(repoRoot, Dir, ConfigFile)
}

// LocalPath returns the absolute path to local.toml for repoRoot.
func LocalPath(repoRoot string) string {
	return filepath.Join(repoRoot, Dir, LocalFile)
}
