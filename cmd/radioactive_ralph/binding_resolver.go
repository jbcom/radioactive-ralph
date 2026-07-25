package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

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

// providersConfigKey selects a Ralph-managed provider pool. Unlike the
// singular provider key, a pool deliberately disables each selected
// binding's NativeFanout capability: one ready plan step becomes one Ralph
// worker, and successive workers are distributed across the configured
// providers. That is what makes a 16-step parallel plan visibly become 16
// independently supervised workers rather than one provider invocation that
// may or may not create opaque subagents of its own.
const providersConfigKey = "providers"

// storeBindingResolver returns an orch.BindingResolver that selects the
// provider from stored virtual config for the dispatch's project, instead of
// the orchestrator's built-in always-claude default. It resolves the
// effective config for the project (DB layers only — the headless supervisor
// has no --config-file/--user-config-file flags to thread) and reads
// providersConfigKey first, then providerConfigKey. An unset key falls back to
// provider.ResolveBinding's own "claude" default, so a project with no provider
// configured still works.
//
// The per-project cursor is guarded because dispatches are asynchronous even
// though DispatchNext currently serializes claim passes. Keeping the resolver
// independently safe prevents a later caller from introducing a data race.
func storeBindingResolver(st *store.Store) func(ctx context.Context, projectID string, parallelGroup bool) (provider.Binding, error) {
	var mu sync.Mutex
	nextByProject := map[string]uint64{}

	return func(ctx context.Context, projectID string, _ bool) (provider.Binding, error) {
		names, pooled, err := resolveProviderNames(ctx, st, projectID)
		if err != nil {
			return provider.Binding{}, err
		}

		name := ""
		if len(names) > 0 {
			mu.Lock()
			cursor := nextByProject[projectID]
			name = names[cursor%uint64(len(names))]
			nextByProject[projectID] = cursor + 1
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
		return binding, nil
	}
}

// resolveProviderNames reads the effective provider selection for a project
// from the virtual-config layer. A plural providers array is a Ralph-managed
// round-robin pool and wins over the backward-compatible singular provider
// key. An entirely unset selection returns no names (not an error), letting
// ResolveBinding apply its built-in default.
func resolveProviderNames(ctx context.Context, st *store.Store, projectID string) ([]string, bool, error) {
	// No file overrides: the supervisor runs headless, so only the DB-backed
	// user and project layers contribute.
	userCfg, err := vconfig.ResolveUser(ctx, st, "", "")
	if err != nil {
		return nil, false, fmt.Errorf("resolve user config: %w", err)
	}
	projectsCfg, err := vconfig.ResolveProjects(ctx, st, userCfg, projectID)
	if err != nil {
		return nil, false, fmt.Errorf("resolve project config: %w", err)
	}
	// ModeOverride: resolve the effective config at runtime without
	// persisting anything (the supervisor is only reading which provider to
	// use, not mutating stored config).
	effective, err := vconfig.EffectiveProject(ctx, st, projectsCfg, projectID, "", vconfig.ModeOverride)
	if err != nil {
		return nil, false, fmt.Errorf("resolve effective project config: %w", err)
	}

	if raw, ok := effective.Values[providersConfigKey]; ok {
		names, err := stringSliceValue(raw)
		if err != nil {
			return nil, false, fmt.Errorf("resolve %s: %w", providersConfigKey, err)
		}
		return names, true, nil
	}
	if v, ok := stringValue(effective.Values[providerConfigKey]); ok {
		return []string{v}, false, nil
	}
	// ResolveProjects normally carries USER values into effective, but retain
	// the explicit fallback for compatibility with stores created by early
	// supervisor builds.
	if raw, ok := userCfg.Values[providersConfigKey]; ok {
		names, err := stringSliceValue(raw)
		if err != nil {
			return nil, false, fmt.Errorf("resolve user %s: %w", providersConfigKey, err)
		}
		return names, true, nil
	}
	if v, ok := stringValue(userCfg.Values[providerConfigKey]); ok {
		return []string{v}, false, nil
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
