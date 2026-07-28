package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jbcom/radioactive-ralph/internal/orch"
	"github.com/jbcom/radioactive-ralph/internal/provider"
	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/vconfig"
)

// providerConfigKey is the config key selecting which provider a dispatch
// binds to. Resolved through the virtual-config layer, so a per-project
// override (project_config row or a [projects.<id>] stanza) beats the
// user-scope default, which beats the built-in "claude". The key is the same
// at both scopes — the layering, not a different name, expresses precedence.
const providerConfigKey = "provider"

// providersConfigKey selects one or more providers. A multi-provider selection
// is a Ralph-managed pool and deliberately disables each selected binding's
// NativeFanout capability: one ready plan step becomes one Ralph worker, and
// successive workers are distributed across the configured providers. A
// one-element canonical selection retains ordinary single-provider semantics,
// including NativeFanout.
const providersConfigKey = "providers"

const (
	turnTimeoutConfigKey  = "turn_timeout"
	stallTimeoutConfigKey = "stall_timeout"
)

// storeBindingResolver returns an orch.BindingResolver that selects the
// provider from stored virtual config for the dispatch's project, instead of
// the orchestrator's built-in always-claude default. It resolves the
// effective config for the project (DB layers only — the headless supervisor
// has no --config-file/--user-config-file flags to thread) and reads
// the canonical providersConfigKey, with providerConfigKey accepted only as a
// legacy one-element alias. An unset selection falls back to
// provider.ResolveBinding's own "claude" default, so a project with no provider
// configured still works.
//
// The per-project cursor is guarded because dispatches are asynchronous even
// though DispatchNext currently serializes claim passes. Keeping the resolver
// independently safe prevents a later caller from introducing a data race.
func storeBindingResolver(st *store.Store) orch.BindingResolver {
	var mu sync.Mutex
	nextByProject := map[string]uint64{}

	return func(ctx context.Context, projectID string, _ bool, purpose orch.BindingResolutionPurpose) (provider.Binding, error) {
		names, pooled, err := resolveProviderNames(ctx, st, projectID)
		if err != nil {
			return provider.Binding{}, err
		}

		name := ""
		if len(names) > 0 {
			mu.Lock()
			cursor := nextByProject[projectID]
			name = names[cursor%uint64(len(names))]
			if purpose == orch.BindingDispatch {
				nextByProject[projectID] = cursor + 1
			}
			mu.Unlock()
		}

		binding, err := provider.ResolveBinding(
			provider.File{DefaultProvider: name},
			provider.Local{},
			provider.VariantFile{},
		)
		if err != nil {
			return provider.Binding{}, err
		}
		if pooled {
			binding.Config.NativeFanout = false
		}
		turnTimeout, stallTimeout, err := resolveProviderTimeouts(ctx, st, projectID)
		if err != nil {
			return provider.Binding{}, err
		}
		binding.Config.TurnTimeout = turnTimeout
		binding.Config.StallTimeout = stallTimeout
		return binding, nil
	}
}

func resolveProviderTimeouts(ctx context.Context, st *store.Store, projectID string) (string, string, error) {
	userCfg, err := vconfig.ResolveUser(ctx, st, "", "")
	if err != nil {
		return "", "", fmt.Errorf("resolve user provider timeouts: %w", err)
	}
	projectCfg, err := vconfig.ResolveProjects(ctx, st, userCfg, projectID)
	if err != nil {
		return "", "", fmt.Errorf("resolve project provider timeouts: %w", err)
	}
	turnTimeout, err := layeredTimeoutValue(turnTimeoutConfigKey, "turn_timeout", userCfg.Values, projectCfg.Values)
	if err != nil {
		return "", "", err
	}
	stallTimeout, err := layeredTimeoutValue(stallTimeoutConfigKey, "stall_timeout", userCfg.Values, projectCfg.Values)
	if err != nil {
		return "", "", err
	}
	return turnTimeout, stallTimeout, nil
}

// layeredTimeoutValue resolves the last-writer-wins configured timeout across
// layers and validates it against the provider's own bounds.
//
// The validation is the load-bearing part: dispatch admission claims a task
// before the runner resolves its limits, so an unparseable or out-of-bounds
// value ("banana", "25h") would let the orchestrator claim the task and launch
// its goroutine, fail inside the runner, and leave the task running until stale
// reclamation — which then repeats the identical cycle without ever making
// progress. Rejecting here means a bad config fails binding resolution, before
// anything is claimed. field names which ceiling applies.
func layeredTimeoutValue(key, field string, layers ...map[string]any) (string, error) {
	var resolved string
	for _, layer := range layers {
		value, exists := layer[key]
		if !exists {
			continue
		}
		timeout, ok := stringValue(value)
		if !ok {
			return "", fmt.Errorf("%s must be a non-empty duration string", key)
		}
		resolved = timeout
	}
	if err := provider.ValidateConfiguredTimeout(field, resolved); err != nil {
		return "", fmt.Errorf("%s: %w", key, err)
	}
	return resolved, nil
}

// resolveProviderNames reads the effective provider selection in source
// precedence order instead of flattening differently named aliases into one
// map. A project-stanza legacy singular selection must override a stored or
// user-scope canonical pool; checking each layer independently preserves that
// relationship while old databases migrate to the canonical providers key.
// An entirely unset selection returns no names (not an error), letting
// ResolveBinding apply its built-in default.
func resolveProviderNames(ctx context.Context, st *store.Store, projectID string) ([]string, bool, error) {
	// No file overrides: the supervisor runs headless, so only the DB-backed
	// user and project layers contribute.
	userCfg, err := vconfig.ResolveUser(ctx, st, "", "")
	if err != nil {
		return nil, false, fmt.Errorf("resolve user config: %w", err)
	}
	return resolveProviderNamesFromUserConfig(ctx, st, userCfg, projectID)
}

func resolveProviderNamesFromUserConfig(ctx context.Context, st *store.Store, userCfg vconfig.UserConfig, projectID string) ([]string, bool, error) {
	return resolveProviderNamesFromUserConfigSource(ctx, vconfig.NewStoreConfigSource(st), userCfg, projectID)
}

// resolveProviderNamesFromUserConfigSource is the ConfigSource form, so a
// client resolving over the supervisor socket runs the same selection logic.
func resolveProviderNamesFromUserConfigSource(ctx context.Context, src vconfig.ConfigSource, userCfg vconfig.UserConfig, projectID string) ([]string, bool, error) {
	// A per-project stanza is the highest project layer. Resolve it before the
	// stored project baseline so an alias change across layers still obeys
	// precedence (provider="codex" overrides providers=[...] below it).
	if projectOverlay, ok := userCfg.Projects[projectID]; ok {
		names, found, err := providerSelectionFromValues(projectOverlay)
		if err != nil {
			return nil, false, fmt.Errorf("resolve project provider selection: %w", err)
		}
		if found {
			return names, len(names) > 1, nil
		}
	}

	storedProject, err := vconfig.ResolveProjectsFrom(ctx, src, vconfig.UserConfig{}, projectID)
	if err != nil {
		return nil, false, fmt.Errorf("resolve stored project config: %w", err)
	}
	names, found, err := providerSelectionFromValues(storedProject.Values)
	if err != nil {
		return nil, false, fmt.Errorf("resolve stored provider selection: %w", err)
	}
	if found {
		return names, len(names) > 1, nil
	}

	names, found, err = providerSelectionFromValues(userCfg.Values)
	if err != nil {
		return nil, false, fmt.Errorf("resolve user provider selection: %w", err)
	}
	if found {
		return names, len(names) > 1, nil
	}
	return nil, false, nil
}

// stringValue coerces a config value to a non-empty string, reporting
// ok=false for a missing key or a non-string/empty value.
func stringValue(v any) (string, bool) {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", false
	}
	return strings.TrimSpace(s), true
}

// stringSliceValue accepts the concrete array shapes produced by Viper and
// encoding/json. Empty names and duplicates are rejected: either would make
// pool distribution ambiguous and hide a configuration error.
func stringSliceValue(v any) ([]string, error) {
	var raw []any
	switch values := v.(type) {
	case []string:
		raw = make([]any, len(values))
		for i := range values {
			raw[i] = values[i]
		}
	case []any:
		raw = values
	default:
		return nil, fmt.Errorf("must be a non-empty array of provider names")
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("must contain at least one provider name")
	}

	names := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for i, value := range raw {
		name, ok := stringValue(value)
		if !ok {
			return nil, fmt.Errorf("entry %d must be a non-empty string", i)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("provider %q is duplicated", name)
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}
