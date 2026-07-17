package provider

// This file holds the provider capability record — spec
// docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md §9:
// "Each provider profile is a CAPABILITY RECORD, not a persona: the binary,
// how it is invoked non-interactively, how its structured result and
// usage/cost are read, how it resumes, and crucially whether the CLI/API
// natively supports subagents/workflows/parallelism."
//
// It supersedes the Phase 4 stand-in (formerly localtypes.go, whose
// TODO(phase-5) header this comment discharges): BindingConfig now carries
// the NativeFanout capability flag decided in
// .agent-state/decisions.ndjson ("Agent-CLI capability profiles must record
// native subagent/workflow/parallelism support"), and File/Local/VariantFile
// are the provider package's own minimal config surface until a later phase
// wires binding resolution directly against internal/vconfig's virtual
// layers (config.File/config.Local do not exist anymore; see internal/vconfig).

// Model is a provider-neutral model-tier selector.
type Model string

// Model tiers. Values match the retired variant.Model constants so
// existing provider config (haiku_model/sonnet_model/opus_model keys)
// keeps meaning the same thing.
const (
	ModelHaiku  Model = "haiku"
	ModelSonnet Model = "sonnet"
	ModelOpus   Model = "opus"
)

// BindingConfig is one provider's capability record: what binary to run,
// how to invoke it non-interactively, how to read back its structured
// result and resume a session, and whether it can fan out work itself.
//
// This is NOT a persona/variant — variants are removed entirely per spec
// §10. A BindingConfig describes only what the underlying CLI/API can do.
type BindingConfig struct {
	Type   string   `toml:"type"`
	Bin    string   `toml:"bin"`
	Binary string   `toml:"binary"`
	Args   []string `toml:"args"`

	// OutputFile is a declarative-runner args token target (see
	// declarative.go); it is unrelated to the agent.Options.ResultPath
	// hybrid-I/O path used by the claude/codex/opencode runners.
	OutputFile string `toml:"output_file"`

	TurnTimeout    string `toml:"turn_timeout"`
	MaxRetries     int    `toml:"max_retries"`
	SessionIDRegex string `toml:"session_id_regex"`

	HaikuModel  string `toml:"haiku_model"`
	SonnetModel string `toml:"sonnet_model"`
	OpusModel   string `toml:"opus_model"`

	LowEffort    string `toml:"low_effort"`
	MediumEffort string `toml:"medium_effort"`
	HighEffort   string `toml:"high_effort"`
	MaxEffort    string `toml:"max_effort"`

	SupportsResume        *bool `toml:"supports_resume"`
	UseAppendSystemPrompt *bool `toml:"use_append_system_prompt"`

	// NativeFanout is true when the bound CLI/API can itself fan out
	// subagents, workflows, or parallel work — spec §9/§10: the
	// orchestrator uses this to decide whether to delegate a parallel
	// step-group to one fan-out-capable agent rather than spawning N
	// Ralph-managed workers. Evidence per provider is documented next to
	// each Default*Provider constructor below. Providers with unverified
	// or absent fan-out support default to false — the flag must never
	// be optimistically set.
	NativeFanout bool `toml:"native_fanout"`
}

// File is the provider package's own minimal config surface: enough for
// ResolveBinding to read DefaultProvider and look up a named provider's
// BindingConfig. A later phase may replace this with a direct
// internal/vconfig-backed decode; the shape here matches what committed
// config.toml historically expressed for the equivalent keys.
type File struct {
	DefaultProvider string
	Providers       map[string]BindingConfig
}

// Local is the provider package's local-override surface: just enough for
// ResolveBinding's local-binary-override lookup (the gitignored local.toml
// escape hatch for pointing a provider at a non-shipped binary).
type Local struct {
	ProviderBinary   string
	ProviderBinaries map[string]string
}

// BinaryFor resolves the local-override binary for providerName. A
// per-provider override takes precedence over the single legacy
// ProviderBinary field.
func (l Local) BinaryFor(providerName string) (string, bool) {
	if bin, ok := l.ProviderBinaries[providerName]; ok && bin != "" {
		return bin, true
	}
	if l.ProviderBinary != "" {
		return l.ProviderBinary, true
	}
	return "", false
}

// VariantFile is the provider package's per-binding-request input — despite
// the name (kept for config-key compatibility with existing committed
// config.toml files), it carries no persona: it is just the provider
// override for one binding request.
type VariantFile struct {
	Provider string
}

// defaultClaudeProvider is claude's capability record.
//
// NativeFanout: true. Evidence (verified against the installed `claude`
// 2.1.211 CLI's --help on 2026-07-16): `--agents <json>` defines custom
// subagents, `--agent <agent>` selects one, `--forward-subagent-text`
// forwards subagent text/thinking blocks, and `--bg/--background` plus the
// `claude agents` subcommand manage background agents — the CLI natively
// fans out and manages subagents (the Task-tool surface), so the
// orchestrator may delegate a parallel step-group to one claude invocation
// instead of spawning N Ralph-managed workers.
func defaultClaudeProvider() BindingConfig {
	return BindingConfig{Type: "claude", Binary: "claude", NativeFanout: true}
}

// defaultCodexProvider is codex's capability record.
//
// NativeFanout: false, UNCONFIRMED. Evidence (installed `codex-cli` 0.142.0
// on 2026-07-16): `codex exec --help` and `codex --help` expose no
// subagent/parallel-workflow flag — `codex exec` runs one turn against one
// model with no documented fan-out primitive. This is a "no evidence found"
// result, not a verified negative; if a future codex release adds a
// subagent/workflow surface, flip this and cite the new flag here.
func defaultCodexProvider() BindingConfig {
	return BindingConfig{Type: "codex", Binary: "codex", NativeFanout: false}
}

// defaultOpencodeProvider is opencode's capability record.
//
// NativeFanout: true. Evidence (installed `opencode` 1.18.3 on
// 2026-07-16): `opencode run --agent <agent>` selects among agents, and
// `opencode agent create`/`opencode agent list` manage a native
// multi-agent surface independent of Ralph's own worker-spawning — the CLI
// natively fans out to its own agents, so the orchestrator may delegate a
// parallel step-group to one opencode invocation instead of spawning N
// Ralph-managed workers.
func defaultOpencodeProvider() BindingConfig {
	return BindingConfig{Type: "opencode", Binary: "opencode", NativeFanout: true}
}

// defaultAgyProvider exists only for tests/documentation purposes: the agy
// spike (see agy.go) concluded `agy --print` is NOT local-only — it drives
// a cloud-backed Cloud Code/Antigravity conversation
// (cloudcode-pa.googleapis.com) and fails without a resolvable cloud
// project ID even when a model is pinned. Per spec §9 ("local-only" = no
// cloud control surface in the loop), agy is therefore Unknown, not
// Supported, and NO runner is registered for it (see agentdetect.Detect).
// This constructor is kept only so the capability-record shape and
// evidence are documented in one place next to its siblings; NewRunner
// deliberately does not wire it up.
func defaultAgyProvider() BindingConfig {
	return BindingConfig{Type: "agy", Binary: "agy", NativeFanout: false}
}
