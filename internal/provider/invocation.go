package provider

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Invocation is the exact tuple placed on a provider command line. Empty
// Effort represents an explicitly calibrated provider-default lane.
type Invocation struct {
	Alias    string
	Provider string
	Model    string
	Effort   string
}

// InvocationConfigHash fingerprints every binding configuration value that can
// alter the command line, plus the exact requested model/effort.
func InvocationConfigHash(binding Binding, model Model, effort string) (string, error) {
	raw, err := json.Marshal(struct {
		Alias  string        `json:"alias"`
		Config BindingConfig `json:"config"`
		Model  Model         `json:"model"`
		Effort string        `json:"effort"`
	}{
		Alias: binding.Name, Config: binding.Config, Model: model, Effort: effort,
	})
	if err != nil {
		return "", fmt.Errorf("provider: marshal invocation config: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

// ResolveInvocation resolves a request without silently falling back.
func ResolveInvocation(binding Binding, req Request) (Invocation, error) {
	if req.StrictBinding {
		if err := validateExactShippedTuple(
			binding.Config.Type, string(req.Model), req.Effort,
		); err != nil {
			return Invocation{}, err
		}
	}
	model := resolveModel(binding.Config, req.Model)
	if req.StrictBinding && req.Model != "" && model == "" {
		return Invocation{}, fmt.Errorf(
			"provider: binding %s cannot resolve requested model %q",
			binding.Name, req.Model,
		)
	}
	effort := resolveEffort(binding.Config, req.Effort)
	if req.StrictBinding && req.Effort != "" && req.Effort != "default" && effort == "" {
		return Invocation{}, fmt.Errorf(
			"provider: binding %s cannot resolve requested effort %q",
			binding.Name, req.Effort,
		)
	}
	if strings.TrimSpace(model) != model || strings.TrimSpace(effort) != effort {
		return Invocation{}, fmt.Errorf("provider: model and effort must not contain surrounding whitespace")
	}
	return Invocation{
		Alias: binding.Name, Provider: binding.Config.Type,
		Model: model, Effort: effort,
	}, nil
}

func validateExactShippedTuple(providerType, model, effort string) error {
	if model == "" || effort == "" {
		return fmt.Errorf("provider: strict binding requires explicit model and effort")
	}
	var modelOK bool
	var efforts []string
	switch providerType {
	case "", "claude":
		modelOK = model == "haiku" || model == "sonnet" || model == "opus" ||
			strings.HasPrefix(model, "claude-")
		efforts = []string{"low", "medium", "high", "xhigh", "max"}
	case "codex":
		modelOK = strings.HasPrefix(model, "gpt-")
		efforts = []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	case "opencode":
		parts := strings.Split(model, "/")
		modelOK = len(parts) == 2 && parts[0] != "" && parts[1] != ""
		efforts = []string{"default"}
	default:
		// Declarative providers own their model vocabulary in their config.
		modelOK = true
		efforts = []string{effort}
	}
	if !modelOK {
		return fmt.Errorf(
			"provider: model %q is not an exact %s model identifier",
			model, providerType,
		)
	}
	if !slices.Contains(efforts, effort) {
		return fmt.Errorf(
			"provider: effort %q is not supported by exact %s bindings",
			effort, providerType,
		)
	}
	return nil
}
