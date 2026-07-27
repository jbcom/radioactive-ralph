package provider

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrBindingCannotHonorRequest reports a StrictBinding request the binding
// cannot satisfy exactly. Distinct from an I/O fault: the binding is fine, it
// simply does not map what was asked for.
var ErrBindingCannotHonorRequest = errors.New("provider: binding cannot honor the request exactly")

// Invocation is the exact tuple that will be placed on a provider command line.
//
// It exists because "what was requested" and "what will run" are different
// things, and until now only the former was recorded. A caller asking for the
// "opus" TIER has no way to learn which concrete model its binding resolved
// that to, so provenance captured the request rather than the reality.
//
// An empty Effort is meaningful: it is a binding that runs at its provider's
// own default, not a missing value.
type Invocation struct {
	Alias    string
	Provider string
	Model    string
	Effort   string
}

// ResolveInvocation resolves what a request will actually run as.
//
// By default it reproduces today's LOOSE resolution exactly — every existing
// plan depends on tiers resolving with fallbacks, and changing that silently
// would change what those plans run. What it adds is visibility: the result
// names the concrete model and effort.
//
// With Request.StrictBinding it additionally REFUSES a request the binding
// cannot honor exactly. That matters because the loose path substitutes
// silently: resolveModel treats the sonnet override as a general fallback, so a
// codex binding configured only with SonnetModel="gpt-5" answers a request for
// OPUS with "gpt-5" and no error. A task pinned to a model then runs on a
// different one with nothing reporting it, which defeats the point of pinning.
func ResolveInvocation(binding Binding, req Request) (Invocation, error) {
	model := resolveModel(binding.Config, req.Model)
	effort := resolveEffort(binding.Config, req.Effort)

	if req.StrictBinding {
		if err := validateExactlyHonorable(binding, req, model); err != nil {
			return Invocation{}, err
		}
	}

	// Whitespace would silently produce a different argv token than intended,
	// and a command line is not a place to be forgiving about that.
	if strings.TrimSpace(model) != model || strings.TrimSpace(effort) != effort {
		return Invocation{}, fmt.Errorf(
			"provider: binding %s resolved model/effort with surrounding whitespace", binding.Name)
	}

	return Invocation{
		Alias:    binding.Name,
		Provider: binding.Config.Type,
		Model:    model,
		Effort:   effort,
	}, nil
}

// validateExactlyHonorable checks that the binding's own configuration maps the
// requested tier and effort, rather than that they appear in a hardcoded list.
//
// Deliberately config-driven: BindingConfig already declares each provider's
// vocabulary (HaikuModel/SonnetModel/OpusModel, Low/Medium/High/MaxEffort), and
// a second hardcoded table of known models would be a duplicate that drifts the
// first time a provider ships a new one. The binding is the authority on what
// it can run.
func validateExactlyHonorable(binding Binding, req Request, model string) error {
	if model == "" {
		return fmt.Errorf("%w: binding %s resolves no model for tier %q",
			ErrBindingCannotHonorRequest, binding.Name, req.Model)
	}
	if req.Model != "" && !bindingMapsModel(binding.Config, req.Model) {
		return fmt.Errorf("%w: binding %s does not map tier %q (it resolved to %q via fallback)",
			ErrBindingCannotHonorRequest, binding.Name, req.Model, model)
	}
	if req.Effort != "" && !bindingMapsEffort(binding.Config, req.Effort) {
		return fmt.Errorf("%w: binding %s does not map effort %q",
			ErrBindingCannotHonorRequest, binding.Name, req.Effort)
	}
	return nil
}

// bindingMapsModel reports whether cfg explicitly maps the requested tier.
//
// A native claude binding maps every tier by construction — the tier names ARE
// its model names — which is why resolveModel passes them through unchanged for
// that type.
func bindingMapsModel(cfg BindingConfig, model Model) bool {
	switch cfg.Type {
	case "", "claude":
		return true
	}
	switch model {
	case ModelHaiku:
		return cfg.HaikuModel != ""
	case ModelSonnet:
		return cfg.SonnetModel != ""
	case ModelOpus:
		return cfg.OpusModel != ""
	default:
		// An explicit non-tier model is honored as written.
		return model != ""
	}
}

// bindingMapsEffort reports whether cfg explicitly maps the requested effort.
//
// "default" is always honorable: it names the provider's own default lane
// rather than a value Ralph has to translate.
func bindingMapsEffort(cfg BindingConfig, effort string) bool {
	switch effort {
	case "", "default":
		return true
	case "low":
		return cfg.LowEffort != ""
	case "medium":
		return cfg.MediumEffort != ""
	case "high":
		return cfg.HighEffort != ""
	case "max":
		return cfg.MaxEffort != ""
	default:
		// resolveEffort passes an unrecognized effort straight through to the
		// command line. Under a strict binding that is exactly what must not
		// happen: the provider may reject it, or worse, ignore it.
		return false
	}
}

// InvocationConfigHash fingerprints every binding value that can alter the
// command line, plus the exact requested model and effort.
//
// A calibration is a measurement of ONE command line. Reusing it across a
// different binary, different args, or a different model would report a
// capability the running configuration was never observed to have — so the
// hash covers the whole config, not just the parts that look interesting.
func InvocationConfigHash(binding Binding, model Model, effort string) (string, error) {
	raw, err := json.Marshal(struct {
		Alias  string        `json:"alias"`
		Config BindingConfig `json:"config"`
		Model  Model         `json:"model"`
		Effort string        `json:"effort"`
	}{
		Alias:  binding.Name,
		Config: binding.Config,
		Model:  model,
		Effort: effort,
	})
	if err != nil {
		return "", fmt.Errorf("provider: marshal invocation config: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}
