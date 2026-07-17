package main

import (
	"context"
	"fmt"

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

// storeBindingResolver returns an orch.BindingResolver that selects the
// provider from stored virtual config for the dispatch's project, instead of
// the orchestrator's built-in always-claude default. It resolves the
// effective config for the project (DB layers only — the headless supervisor
// has no --config-file/--user-config-file flags to thread) and reads
// providerConfigKey; an unset key falls back to provider.ResolveBinding's own
// "claude" default, so a project with no provider configured still works.
func storeBindingResolver(st *store.Store) func(ctx context.Context, projectID string, parallelGroup bool) (provider.Binding, error) {
	return func(ctx context.Context, projectID string, _ bool) (provider.Binding, error) {
		name, err := resolveProviderName(ctx, st, projectID)
		if err != nil {
			return provider.Binding{}, err
		}
		return provider.ResolveBinding(
			provider.File{DefaultProvider: name},
			provider.Local{},
			provider.VariantFile{},
		)
	}
}

// resolveProviderName reads the effective provider name for a project from
// the virtual-config layer. Returns "" (not an error) when no provider key
// is configured, letting ResolveBinding apply its built-in default.
func resolveProviderName(ctx context.Context, st *store.Store, projectID string) (string, error) {
	// No file overrides: the supervisor runs headless, so only the DB-backed
	// user and project layers contribute.
	userCfg, err := vconfig.ResolveUser(ctx, st, "", "")
	if err != nil {
		return "", fmt.Errorf("resolve user config: %w", err)
	}
	projectsCfg, err := vconfig.ResolveProjects(ctx, st, userCfg, projectID)
	if err != nil {
		return "", fmt.Errorf("resolve project config: %w", err)
	}
	// ModeOverride: resolve the effective config at runtime without
	// persisting anything (the supervisor is only reading which provider to
	// use, not mutating stored config).
	effective, err := vconfig.EffectiveProject(ctx, st, projectsCfg, projectID, "", vconfig.ModeOverride)
	if err != nil {
		return "", fmt.Errorf("resolve effective project config: %w", err)
	}

	// Project-scope value wins; fall back to the user-scope default.
	if v, ok := stringValue(effective.Values[providerConfigKey]); ok {
		return v, nil
	}
	if v, ok := stringValue(userCfg.Values[providerConfigKey]); ok {
		return v, nil
	}
	return "", nil
}

// stringValue coerces a config value to a non-empty string, reporting
// ok=false for a missing key or a non-string/empty value.
func stringValue(v any) (string, bool) {
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}
