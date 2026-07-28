package provider

import (
	"errors"
	"strings"
	"testing"
)

// TestResolveInvocationReportsWhatWillActuallyRun is the point of the type. A
// caller that asked for a tier ("opus") has no way today to learn which
// concrete model the binding resolved it to, so provenance records what was
// REQUESTED rather than what ran.
func TestResolveInvocationReportsWhatWillActuallyRun(t *testing.T) {
	binding := Binding{
		Name: "codex-pool",
		Config: BindingConfig{
			Type: "codex", Binary: "codex",
			SonnetModel: "gpt-5", HighEffort: "high",
		},
	}
	inv, err := ResolveInvocation(binding, Request{Model: ModelSonnet, Effort: "high"})
	if err != nil {
		t.Fatalf("ResolveInvocation: %v", err)
	}
	if inv.Alias != "codex-pool" || inv.Provider != "codex" {
		t.Errorf("invocation identity = %+v, want the alias and provider type", inv)
	}
	if inv.Model != "gpt-5" {
		t.Errorf("Model = %q, want the CONCRETE model the tier resolved to", inv.Model)
	}
	if inv.Effort != "high" {
		t.Errorf("Effort = %q, want the resolved effort", inv.Effort)
	}
}

// TestStrictBindingRefusesASilentModelSubstitution is the bug this increment
// exists to close, and it is live on main rather than theoretical.
//
// resolveModel treats the sonnet override as a general fallback, so a codex
// binding configured only with SonnetModel="gpt-5" answers a request for OPUS
// with "gpt-5" — and returns no error. A task pinned to a model runs on a
// different one and nothing reports it, which defeats the entire point of
// pinning.
//
// Without StrictBinding the fallback stays (existing plans rely on tiers
// resolving loosely); with it, the substitution is refused.
func TestStrictBindingRefusesASilentModelSubstitution(t *testing.T) {
	binding := Binding{
		Name:   "codex-pool",
		Config: BindingConfig{Type: "codex", Binary: "codex", SonnetModel: "gpt-5"},
	}

	// Loose (today's behavior): the substitution happens silently.
	loose, err := ResolveInvocation(binding, Request{Model: ModelOpus, Effort: "high"})
	if err != nil {
		t.Fatalf("loose ResolveInvocation: %v", err)
	}
	if loose.Model != "gpt-5" {
		t.Fatalf("loose Model = %q, want the documented fallback to gpt-5", loose.Model)
	}

	// Strict: a request that cannot be honored exactly must FAIL.
	_, err = ResolveInvocation(binding, Request{
		Model: ModelOpus, Effort: "high", StrictBinding: true,
	})
	if err == nil {
		t.Fatal("strict binding accepted a request for opus against a binding that " +
			"only knows gpt-5; a pinned task would silently run on the wrong model")
	}
	if !errors.Is(err, ErrBindingCannotHonorRequest) {
		t.Fatalf("err = %v, want ErrBindingCannotHonorRequest so callers can tell "+
			"an unhonorable pin from an I/O fault", err)
	}
	if !strings.Contains(err.Error(), "opus") {
		t.Errorf("err = %v, want it to name the model that could not be honored", err)
	}
}

// TestStrictBindingRefusesAnUnconfiguredEffort covers the same hazard on the
// effort axis: resolveEffort passes an unrecognized effort straight through, so
// a binding with no xhigh mapping would put "xhigh" on a command line that may
// not accept it.
func TestStrictBindingRefusesAnUnconfiguredEffort(t *testing.T) {
	binding := Binding{
		Name:   "codex-pool",
		Config: BindingConfig{Type: "codex", Binary: "codex", SonnetModel: "gpt-5"},
	}
	_, err := ResolveInvocation(binding, Request{
		Model: ModelSonnet, Effort: "xhigh", StrictBinding: true,
	})
	if err == nil {
		t.Fatal("strict binding accepted an effort the binding does not map")
	}
	if !errors.Is(err, ErrBindingCannotHonorRequest) {
		t.Fatalf("err = %v, want ErrBindingCannotHonorRequest", err)
	}
}

// TestStrictBindingAcceptsAnExactlyHonorableRequest is the control. Strictness
// that refused legitimate requests would be worse than the silent substitution
// it replaces.
func TestStrictBindingAcceptsAnExactlyHonorableRequest(t *testing.T) {
	binding := Binding{
		Name: "codex-pool",
		Config: BindingConfig{
			Type: "codex", Binary: "codex",
			SonnetModel: "gpt-5", HighEffort: "high",
		},
	}
	inv, err := ResolveInvocation(binding, Request{
		Model: ModelSonnet, Effort: "high", StrictBinding: true,
	})
	if err != nil {
		t.Fatalf("strict binding refused an exactly-honorable request: %v", err)
	}
	if inv.Model != "gpt-5" || inv.Effort != "high" {
		t.Fatalf("invocation = %+v, want the configured mapping", inv)
	}
}

// TestLooseResolutionIsUnchanged is the compatibility guard. Every existing
// plan resolves loosely, so introducing Invocation must not change what any of
// them runs — only make it observable.
func TestLooseResolutionIsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cfg    BindingConfig
		model  Model
		effort string
	}{
		{"claude default tier", BindingConfig{Type: "claude"}, ModelSonnet, "high"},
		{"claude passthrough", BindingConfig{Type: "claude"}, ModelOpus, "low"},
		{"codex mapped", BindingConfig{Type: "codex", SonnetModel: "gpt-5"}, ModelSonnet, "medium"},
		{"unconfigured effort", BindingConfig{Type: "claude"}, ModelSonnet, "xhigh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binding := Binding{Name: "b", Config: tc.cfg}
			inv, err := ResolveInvocation(binding, Request{Model: tc.model, Effort: tc.effort})
			if err != nil {
				t.Fatalf("loose resolution errored: %v", err)
			}
			if want := resolveModel(tc.cfg, tc.model); inv.Model != want {
				t.Errorf("Model = %q, want %q (identical to today's resolution)", inv.Model, want)
			}
			if want := resolveEffort(tc.cfg, tc.effort); inv.Effort != want {
				t.Errorf("Effort = %q, want %q (identical to today's resolution)", inv.Effort, want)
			}
		})
	}
}

// TestInvocationConfigHashChangesWithTheCommandLine pins the fingerprint's
// purpose: it identifies the exact configuration a turn ran under, so a
// calibration recorded against one command line cannot be reused for a
// different one.
func TestInvocationConfigHashChangesWithTheCommandLine(t *testing.T) {
	base := Binding{Name: "b", Config: BindingConfig{Type: "codex", Binary: "codex"}}
	baseline, err := InvocationConfigHash(base, ModelSonnet, "high")
	if err != nil {
		t.Fatalf("InvocationConfigHash: %v", err)
	}

	for _, tc := range []struct {
		name    string
		binding Binding
		model   Model
		effort  string
	}{
		{"different alias", Binding{Name: "other", Config: base.Config}, ModelSonnet, "high"},
		{"different args", Binding{Name: "b", Config: BindingConfig{
			Type: "codex", Binary: "codex", Args: []string{"--flag"},
		}}, ModelSonnet, "high"},
		{"different model", base, ModelOpus, "high"},
		{"different effort", base, ModelSonnet, "low"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := InvocationConfigHash(tc.binding, tc.model, tc.effort)
			if err != nil {
				t.Fatalf("InvocationConfigHash: %v", err)
			}
			if got == baseline {
				t.Fatalf("hash unchanged despite a %s; a calibration could be "+
					"reused across different command lines", tc.name)
			}
		})
	}

	// Stability: the same inputs must hash identically, or every lookup misses.
	again, err := InvocationConfigHash(base, ModelSonnet, "high")
	if err != nil {
		t.Fatalf("InvocationConfigHash: %v", err)
	}
	if again != baseline {
		t.Fatal("hash is not stable across calls")
	}
}

// TestStrictClaudeBindingRefusesATierFallback is CodeRabbit's P1 on #234,
// confirmed against the code rather than accepted on assertion.
//
// resolveModel treats SonnetModel as a UNIVERSAL fallback (claude.go:364), not
// just the sonnet tier's mapping. bindingMapsModel short-circuited to true for
// every claude-type binding, so strict mode approved a substitution it exists to
// refuse: a task pinned to opus silently runs on the sonnet override.
func TestStrictClaudeBindingRefusesATierFallback(t *testing.T) {
	binding := Binding{
		Name: "claude-pinned",
		// HighEffort IS mapped, so only the MODEL axis can fail this test —
		// otherwise the effort check catches it and the test passes for the
		// wrong reason.
		Config: BindingConfig{
			Type: "claude", SonnetModel: "claude-sonnet-5", HighEffort: "high",
		},
	}
	for _, model := range []Model{ModelOpus, ModelHaiku} {
		t.Run(string(model), func(t *testing.T) {
			// Loose resolution documents the substitution that actually happens.
			loose, err := ResolveInvocation(binding, Request{Model: model, Effort: "high"})
			if err != nil {
				t.Fatalf("loose ResolveInvocation: %v", err)
			}
			if loose.Model != "claude-sonnet-5" {
				t.Fatalf("loose Model = %q, want the sonnet fallback — if this changed, "+
					"the strict case below is testing something else", loose.Model)
			}

			_, err = ResolveInvocation(binding, Request{
				Model: model, Effort: "high", StrictBinding: true,
			})
			if err == nil {
				t.Fatalf("strict binding accepted %s against a binding that only maps "+
					"sonnet; the task would run on claude-sonnet-5 while its provenance "+
					"claims %s", model, model)
			}
			if !errors.Is(err, ErrBindingCannotHonorRequest) {
				t.Fatalf("err = %v, want ErrBindingCannotHonorRequest", err)
			}
		})
	}
}

// TestStrictBindingRefusesASubstitutedCustomModel covers the same fallback on
// the non-tier axis. A custom model was treated as mapped merely because it was
// non-empty, so a strict request for "gpt-4" against SonnetModel "gpt-5"
// succeeded and ran gpt-5.
func TestStrictBindingRefusesASubstitutedCustomModel(t *testing.T) {
	binding := Binding{
		Name:   "codex-pool",
		Config: BindingConfig{Type: "codex", Binary: "codex", SonnetModel: "gpt-5"},
	}
	_, err := ResolveInvocation(binding, Request{
		Model: Model("gpt-4"), Effort: "default", StrictBinding: true,
	})
	if err == nil {
		t.Fatal("strict binding accepted a custom model the binding substitutes away; " +
			"the task would run gpt-5 while pinned to gpt-4")
	}
	if !errors.Is(err, ErrBindingCannotHonorRequest) {
		t.Fatalf("err = %v, want ErrBindingCannotHonorRequest", err)
	}
}

// TestStrictBindingAcceptsAnExactCustomModelOnAPassThroughBinding is the
// control for the substitution check above: a custom model that survives
// resolution UNCHANGED must still be honorable, or strict mode would refuse
// every non-tier pin.
//
// A claude-type binding is the pass-through case — resolveModel returns the
// requested name verbatim when no override displaces it. A codex binding cannot
// appear here: with no SonnetModel it resolves a custom model to "" (nothing to
// run), and with one it substitutes, so neither shape honors a custom pin.
func TestStrictBindingAcceptsAnExactCustomModelOnAPassThroughBinding(t *testing.T) {
	binding := Binding{
		Name:   "claude-custom",
		Config: BindingConfig{Type: "claude"},
	}
	inv, err := ResolveInvocation(binding, Request{
		Model: Model("claude-opus-5-20260101"), Effort: "default", StrictBinding: true,
	})
	if err != nil {
		t.Fatalf("strict binding refused a model it resolves exactly: %v", err)
	}
	if inv.Model != "claude-opus-5-20260101" {
		t.Fatalf("Model = %q, want the requested model unchanged", inv.Model)
	}
}

// TestStrictClaudeBindingAcceptsAnExactTierMapping is the control proving the
// fix does not simply refuse every claude strict request.
func TestStrictClaudeBindingAcceptsAnExactTierMapping(t *testing.T) {
	binding := Binding{
		Name: "claude-pinned",
		Config: BindingConfig{
			Type: "claude", OpusModel: "claude-opus-5", HighEffort: "high",
		},
	}
	inv, err := ResolveInvocation(binding, Request{
		Model: ModelOpus, Effort: "high", StrictBinding: true,
	})
	if err != nil {
		t.Fatalf("strict binding refused an exactly-mapped tier: %v", err)
	}
	if inv.Model != "claude-opus-5" {
		t.Fatalf("Model = %q, want the configured opus mapping", inv.Model)
	}
}
