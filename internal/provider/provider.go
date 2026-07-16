// Package provider adapts configured CLI backends into radioactive_ralph's
// provider-neutral worker execution contract.
package provider

import (
	"context"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/config"
	"github.com/jbcom/radioactive-ralph/internal/variant"
)

// Binding is one resolved provider selection after repo config, local
// overrides, and per-variant overrides have been applied.
type Binding struct {
	Name   string
	Config config.ProviderFile

	// BinaryFromLocal is true when Config.Binary was set by the gitignored
	// local.toml provider_binary override rather than by committed
	// config.toml. Committed config may only name a shipped provider
	// binary (claude/codex/gemini); an arbitrary binary must come from
	// local.toml, so a pull request cannot point the runtime at
	// /bin/sh. ValidateBinding enforces this.
	BinaryFromLocal bool
}

// Request is the provider-neutral execution contract for one worker turn.
type Request struct {
	WorkingDir   string
	SystemPrompt string
	UserPrompt   string
	OutputSchema string
	Model        variant.Model
	Effort       string
	AllowedTools []string
}

// Usage captures the token/cost accounting for one provider turn. Fields
// are zero when the provider does not report them. Coverage today: the
// claude runner populates Usage from the stream-json result frame; codex,
// gemini, and declarative bindings report zero (their CLIs surface usage
// differently and are not yet parsed). CostUSD is authoritative when
// non-zero; the runtime accumulates it for spend-cap enforcement, so a
// capped variant on an unreported provider still requires a cap value but
// its cost is not yet metered. Extending codex/gemini parsing is the
// follow-up to close that gap.
type Usage struct {
	InputTokens       int
	OutputTokens      int
	CachedInputTokens int
	CostUSD           float64
}

// Result captures the observable output of one provider turn.
type Result struct {
	SessionID       string
	AssistantOutput string
	Usage           Usage
}

// Runner executes one provider turn.
type Runner interface {
	Run(ctx context.Context, binding Binding, req Request) (Result, error)
}

// ResolveBinding picks the provider for one variant.
func ResolveBinding(cfg config.File, local config.Local, _ variant.Profile, fromConfig config.VariantFile) (Binding, error) {
	name := fromConfig.Provider
	if name == "" {
		name = cfg.DefaultProvider
	}
	if name == "" {
		name = "claude"
	}
	providerCfg, ok := cfg.Providers[name]
	if !ok {
		builtIn, ok := builtInProvider(name)
		if !ok {
			return Binding{}, fmt.Errorf("provider %q not declared in config.toml", name)
		}
		providerCfg = builtIn
	}
	if providerCfg.Type == "" {
		providerCfg.Type = name
	}
	binaryFromLocal := false
	if local.ProviderBinary != "" {
		providerCfg.Binary = local.ProviderBinary
		binaryFromLocal = true
	}
	if providerCfg.Binary == "" {
		if builtIn, ok := builtInProvider(providerCfg.Type); ok {
			providerCfg.Binary = builtIn.Binary
		}
	}
	return Binding{Name: name, Config: providerCfg, BinaryFromLocal: binaryFromLocal}, nil
}

// shippedProviderBinaries are the executable names the built-in provider
// types resolve to. A committed config.toml may name one of these; any
// other binary must come from the gitignored local.toml provider_binary
// override. Keep in sync with config.Default*Provider.
var shippedProviderBinaries = map[string]bool{
	"claude": true,
	"codex":  true,
	"gemini": true,
}

// NewRunner returns the runtime implementation for a provider type.
func NewRunner(binding Binding) (Runner, error) {
	switch binding.Config.Type {
	case "", "claude":
		return ClaudeRunner{}, nil
	case "codex":
		return CodexRunner{}, nil
	case "gemini":
		return GeminiRunner{}, nil
	case declarativePlainStdout, declarativeLastMessageFile, declarativeStreamJSON:
		return DeclarativeRunner{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider type %q", binding.Config.Type)
	}
}

func builtInProvider(name string) (config.ProviderFile, bool) {
	switch name {
	case "", "claude":
		return config.DefaultClaudeProvider(), true
	case "codex":
		return config.DefaultCodexProvider(), true
	case "gemini":
		return config.DefaultGeminiProvider(), true
	default:
		return config.ProviderFile{}, false
	}
}
