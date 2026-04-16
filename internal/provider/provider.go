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

// Result captures the observable output of one provider turn.
type Result struct {
	SessionID       string
	AssistantOutput string
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
	if local.ProviderBinary != "" {
		providerCfg.Binary = local.ProviderBinary
	}
	if providerCfg.Binary == "" {
		if builtIn, ok := builtInProvider(providerCfg.Type); ok {
			providerCfg.Binary = builtIn.Binary
		}
	}
	return Binding{Name: name, Config: providerCfg}, nil
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
