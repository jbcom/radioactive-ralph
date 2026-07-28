// Package provider adapts configured CLI backends into radioactive_ralph's
// provider-neutral worker execution contract.
package provider

import (
	"context"
	"fmt"
	"time"
)

// Binding is one resolved provider selection after repo config, local
// overrides, and per-variant overrides have been applied.
type Binding struct {
	Name   string
	Config BindingConfig

	// BinaryFromLocal is true when Config.Binary was set by the gitignored
	// local.toml provider_binary override rather than by committed
	// config.toml. Committed config may only name a shipped provider
	// binary (claude/codex); an arbitrary binary must come from
	// local.toml, so a pull request cannot point the runtime at
	// /bin/sh. ValidateBinding enforces this.
	BinaryFromLocal bool
}

// Request is the provider-neutral execution contract for one worker turn.
type Request struct {
	WorkingDir string

	// ContainmentRoot, when set, confines the provider process AND everything
	// it spawns to writing beneath this absolute path, enforced by the kernel
	// (see internal/contain).
	//
	// Explicit rather than derived from WorkingDir: where a turn runs is not the
	// same claim as the only place it may write, and silently equating them
	// would change behavior for every existing caller. Empty leaves the process
	// unconfined, exactly as before.
	//
	// A RUNNER THAT DOES NOT PASS THIS TO applyContainment SILENTLY VOIDS THE
	// GUARANTEE. That is not hypothetical: the stream-json declarative shape once
	// dropped it while the other two shapes carried it, so the field was set, the
	// config claimed containment, and the turn wrote wherever it liked. Every
	// exec path must route through applyContainment — which also fails closed on
	// an unsupported platform rather than running unwrapped.
	ContainmentRoot string

	SystemPrompt string
	UserPrompt   string
	OutputSchema string
	Model        Model
	Effort       string
	AllowedTools []string
	// TurnTimeout is the absolute wall-clock ceiling for the complete turn,
	// including provider retries. StallTimeout is the renewable progress
	// lease: output renews it, but never extends TurnTimeout.
	//
	// Zero values inherit the resolved provider/project defaults.
	TurnTimeout  time.Duration
	StallTimeout time.Duration

	// StrictBinding refuses a request the binding cannot honor EXACTLY,
	// instead of letting model/effort resolution fall back.
	//
	// Off by default because every existing plan relies on tiers resolving
	// loosely. On, it closes a real hole: the loose path substitutes silently,
	// so a task pinned to a model can run on a different one with nothing
	// reporting it. See ResolveInvocation.
	StrictBinding bool
}

// Usage captures the token/cost accounting for one provider turn. Fields
// are zero when the provider does not report them. Coverage today: the
// claude and opencode runners populate Usage from their stream-json frames;
// codex and declarative bindings report zero (their CLIs surface usage
// differently and are not yet parsed). CostUSD is authoritative when
// non-zero; the runtime accumulates it for spend-cap enforcement, so a
// capped variant on an unreported provider still requires a cap value but
// its cost is not yet metered. Extending codex parsing is the follow-up
// to close that gap.
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

	// Invocation is what the turn ACTUALLY ran as — the concrete model and
	// effort after resolution, not the tier that was requested. Recorded so
	// provenance reflects reality: "opus" in a plan and "gpt-5" on the command
	// line are both true, and only the second says what produced the result.
	Invocation Invocation
}

// Runner executes one provider turn.
type Runner interface {
	Run(ctx context.Context, binding Binding, req Request) (Result, error)
}

// ResolveBinding picks the provider for one variant.
func ResolveBinding(cfg File, local Local, fromConfig VariantFile) (Binding, error) {
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
	if bin, ok := local.BinaryFor(name); ok {
		providerCfg.Binary = bin
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
// override. Keep in sync with builtInProvider. agy is deliberately absent:
// the spike in agy.go found it is not local-only, so no runner is
// registered for it and it must never be reachable from committed config.
var shippedProviderBinaries = map[string]bool{
	"claude":   true,
	"codex":    true,
	"opencode": true,
}

// NewRunner returns the runtime implementation for a provider type.
func NewRunner(binding Binding) (Runner, error) {
	switch binding.Config.Type {
	case "", "claude":
		return ClaudeRunner{}, nil
	case "codex":
		return CodexRunner{}, nil
	case "opencode":
		return OpencodeRunner{}, nil
	case declarativePlainStdout, declarativeLastMessageFile, declarativeStreamJSON:
		return DeclarativeRunner{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider type %q", binding.Config.Type)
	}
}

func builtInProvider(name string) (BindingConfig, bool) {
	switch name {
	case "", "claude":
		return defaultClaudeProvider(), true
	case "codex":
		return defaultCodexProvider(), true
	case "opencode":
		return defaultOpencodeProvider(), true
	default:
		return BindingConfig{}, false
	}
}
