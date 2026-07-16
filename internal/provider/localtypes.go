package provider

// TODO(phase-5): internal/provider still speaks the retired per-repo
// committed-config model (internal/config, deleted in the supervisor
// rewrite's Phase 4) and the retired persona model (internal/variant,
// slated for full removal per spec §10 "no variants"). Phase 4 deleted
// both packages (superseded by internal/vconfig + the plan/task/worker
// model in internal/store) but left internal/provider untouched per the
// phase directive, so this file is the minimal local stand-in that lets
// the package keep compiling standalone until Phase 5 rewrites provider
// binding resolution against vconfig's virtual-layer config and drops the
// variant-shaped Request.Model field entirely.
//
// BindingConfig mirrors the subset of the old config.ProviderFile shape
// that claude.go/codex.go/declarative.go/exec.go actually read. Model
// replaces variant.Model as the provider-neutral "which model tier" enum
// Request.Model carries; ResolveBinding's third parameter (formerly
// variant.Profile) is dropped in favor of the file+local config shapes
// below since Phase 5 owns choosing its real replacement.

// Model is a provider-neutral model-tier selector. Replaces the deleted
// variant.Model; Phase 5 should reconsider whether this belongs on
// Request at all once binding resolution moves to vconfig.
type Model string

// Model tiers. Values match the retired variant.Model constants so
// existing provider config (haiku_model/sonnet_model/opus_model keys)
// keeps meaning the same thing.
const (
	ModelHaiku  Model = "haiku"
	ModelSonnet Model = "sonnet"
	ModelOpus   Model = "opus"
)

// BindingConfig is the minimal local stand-in for the deleted
// config.ProviderFile. Field set and toml tags are unchanged so a future
// Phase 5 vconfig-backed decode can reuse the same shape.
type BindingConfig struct {
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

// File is the minimal local stand-in for the deleted config.File — just
// enough for ResolveBinding to read DefaultProvider and look up a named
// provider's BindingConfig.
type File struct {
	DefaultProvider string
	Providers       map[string]BindingConfig
}

// Local is the minimal local stand-in for the deleted config.Local — just
// enough for ResolveBinding's local-binary-override lookup.
type Local struct {
	ProviderBinary   string
	ProviderBinaries map[string]string
}

// BinaryFor mirrors the deleted config.Local.BinaryFor: a per-provider
// override takes precedence over the single legacy ProviderBinary field.
func (l Local) BinaryFor(providerName string) (string, bool) {
	if bin, ok := l.ProviderBinaries[providerName]; ok && bin != "" {
		return bin, true
	}
	if l.ProviderBinary != "" {
		return l.ProviderBinary, true
	}
	return "", false
}

// VariantFile is the minimal local stand-in for the deleted
// config.VariantFile — ResolveBinding only reads Provider off it.
type VariantFile struct {
	Provider string
}

// defaultClaudeProvider mirrors the deleted config.DefaultClaudeProvider.
func defaultClaudeProvider() BindingConfig {
	return BindingConfig{Type: "claude", Binary: "claude"}
}

// defaultCodexProvider mirrors the deleted config.DefaultCodexProvider.
func defaultCodexProvider() BindingConfig {
	return BindingConfig{Type: "codex", Binary: "codex"}
}
