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

// providerConfigTableKey is the TOML table namespace ([provider_config.<name>])
// carrying per-provider BindingConfig overrides — model tier mappings
// (haiku_model, sonnet_model, opus_model), effort mappings, and timeout
// overrides. Without reading it, the resolver would select the right provider
// NAME but discard every per-provider capability override, so a project
// configuring opencode to use ollama/cloud models would see opencode run with
// its built-in defaults instead. The key is a TABLE, not an array, so it does
// not collide with the providers array in TOML.
const providerConfigTableKey = "provider_config"

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
func storeBindingResolver(st *store.Store, cooldowns *ProviderCooldowns) orch.BindingResolver {
	var mu sync.Mutex
	nextByProject := map[string]uint64{}

	return func(ctx context.Context, projectID string, _ bool, purpose orch.BindingResolutionPurpose) (provider.Binding, error) {
		names, pooled, err := resolveProviderNames(ctx, st, projectID)
		if err != nil {
			return provider.Binding{}, err
		}

		// Filter out providers whose cooldown hasn't expired. A provider in
		// cooldown failed with provider_auth or provider_rejected (rate-limit
		// / credit exhaustion), and its limits may not have reset yet. If ALL
		// providers are in cooldown, pick the one with the earliest expiry so
		// at least one dispatch can proceed as a probe once its cooldown ends.
		active := cooldowns.Active()
		if len(active) > 0 && len(names) > 1 {
			filtered := make([]string, 0, len(names))
			for _, n := range names {
				if _, cooled := active[n]; !cooled {
					filtered = append(filtered, n)
				}
			}
			if len(filtered) == 0 {
				// All providers are in cooldown — pick the earliest expiring
				// one so a probe can run once its cooldown ends.
				if earliest := cooldowns.EarliestExpiry(); earliest != "" {
					filtered = []string{earliest}
				}
			}
			if len(filtered) > 0 {
				names = filtered
				pooled = len(names) > 1
			}
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

		providerCfgs, err := resolveProviderConfigs(ctx, st, projectID)
		if err != nil {
			return provider.Binding{}, err
		}
		binding, err := provider.ResolveBinding(
			provider.File{DefaultProvider: name, Providers: providerCfgs},
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

// resolveProviderConfigs reads the [provider_config.<name>] table from the
// effective config layers and returns a map of provider name to BindingConfig
// overrides. The overrides are MERGED onto the built-in defaults — only the
// keys present in the stored config replace the built-in fields, so an
// operator who configures only haiku_model gets that override while every
// other field (Binary, WritePaths, SupportsContainment, NativeFanout) comes
// from the built-in capability record. Without this merge, a partial
// override would zero out WritePaths and SupportsContainment, causing
// containment to refuse the turn or crash the provider.
//
// The user and project layers are resolved independently (matching
// resolveProviderNames' source-precedence) and merged last-writer-wins: a
// project-level [provider_config.opencode] overrides a user-level one for the
// keys it specifies, rather than replacing the whole table.
func resolveProviderConfigs(ctx context.Context, st *store.Store, projectID string) (map[string]provider.BindingConfig, error) {
	userCfg, err := vconfig.ResolveUser(ctx, st, "", "")
	if err != nil {
		return nil, fmt.Errorf("resolve user provider configs: %w", err)
	}
	projectCfg, err := vconfig.ResolveProjects(ctx, st, userCfg, projectID)
	if err != nil {
		return nil, fmt.Errorf("resolve project provider configs: %w", err)
	}
	merged := mergeProviderConfigTables(
		extractProviderConfigTable(userCfg.Values),
		extractProviderConfigTable(projectCfg.Values),
	)
	if len(merged) == 0 {
		return nil, nil
	}
	out := make(map[string]provider.BindingConfig, len(merged))
	for name, fields := range merged {
		base, ok := provider.BuiltInProvider(name)
		if !ok {
			// An unknown provider name in provider_config is not a
			// resolution error here — resolveProviderNames already
			// validates the names. A stray entry that no dispatch
			// selects is inert.
			out[name] = bindingConfigFromFields(fields)
			continue
		}
		out[name] = mergeBindingConfigOverrides(base, fields)
	}
	return out, nil
}

// extractProviderConfigTable reads the provider_config key from one config
// layer's Values and coerces it to map[string]map[string]any. Returns nil
// when the key is absent or not a table — a missing namespace is not an error.
func extractProviderConfigTable(values map[string]any) map[string]map[string]any {
	raw, ok := values[providerConfigTableKey]
	if !ok {
		return nil
	}
	table, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]map[string]any, len(table))
	for name, fields := range table {
		fm, ok := fields.(map[string]any)
		if !ok {
			continue
		}
		out[name] = fm
	}
	return out
}

// mergeProviderConfigTables merges a lower-precedence table (user) with a
// higher-precedence one (project), field-by-field per provider. A provider
// present in both gets its fields merged (project wins), not replaced.
func mergeProviderConfigTables(layers ...map[string]map[string]any) map[string]map[string]any {
	merged := map[string]map[string]any{}
	for _, layer := range layers {
		for name, fields := range layer {
			existing := merged[name]
			if existing == nil {
				existing = map[string]any{}
				merged[name] = existing
			}
			for k, v := range fields {
				existing[k] = v
			}
		}
	}
	return merged
}

// bindingConfigFromFields builds a BindingConfig from the string fields the
// [provider_config.<name>] table carries. Only the recognized keys are read;
// unknown keys are silently ignored so a future BindingConfig field can be
// added without breaking configs that predate it.
func bindingConfigFromFields(fields map[string]any) provider.BindingConfig {
	cfg := provider.BindingConfig{}
	cfg.HaikuModel = fieldString(fields, "haiku_model")
	cfg.SonnetModel = fieldString(fields, "sonnet_model")
	cfg.OpusModel = fieldString(fields, "opus_model")
	cfg.LowEffort = fieldString(fields, "low_effort")
	cfg.MediumEffort = fieldString(fields, "medium_effort")
	cfg.HighEffort = fieldString(fields, "high_effort")
	cfg.MaxEffort = fieldString(fields, "max_effort")
	cfg.TurnTimeout = fieldString(fields, "turn_timeout")
	cfg.StallTimeout = fieldString(fields, "stall_timeout")
	if s, ok := fields["native_fanout"]; ok {
		if b, ok := s.(bool); ok {
			cfg.NativeFanout = b
		}
	}
	return cfg
}

// mergeBindingConfigOverrides applies the fields present in the
// [provider_config.<name>] table onto a built-in BindingConfig, preserving
// every unset field from the base. Without this, a partial override (e.g.
// only haiku_model) would zero out WritePaths and SupportsContainment,
// causing containment to refuse or crash a provider that the built-in
// default correctly declared as capable.
func mergeBindingConfigOverrides(base provider.BindingConfig, fields map[string]any) provider.BindingConfig {
	cfg := base
	if s := fieldString(fields, "haiku_model"); s != "" {
		cfg.HaikuModel = s
	}
	if s := fieldString(fields, "sonnet_model"); s != "" {
		cfg.SonnetModel = s
	}
	if s := fieldString(fields, "opus_model"); s != "" {
		cfg.OpusModel = s
	}
	if s := fieldString(fields, "low_effort"); s != "" {
		cfg.LowEffort = s
	}
	if s := fieldString(fields, "medium_effort"); s != "" {
		cfg.MediumEffort = s
	}
	if s := fieldString(fields, "high_effort"); s != "" {
		cfg.HighEffort = s
	}
	if s := fieldString(fields, "max_effort"); s != "" {
		cfg.MaxEffort = s
	}
	if s := fieldString(fields, "turn_timeout"); s != "" {
		cfg.TurnTimeout = s
	}
	if s := fieldString(fields, "stall_timeout"); s != "" {
		cfg.StallTimeout = s
	}
	if s, ok := fields["native_fanout"]; ok {
		if b, ok := s.(bool); ok {
			cfg.NativeFanout = b
		}
	}
	return cfg
}

// fieldString reads a string field from the provider config table, returning
// "" for absent or non-string values. A non-string value for a model or effort
// field is a config error, but failing the whole resolution for one bad field
// would make a single typo block every dispatch — the built-in default fills
// in instead, and the operator sees the wrong model rather than no model.
func fieldString(fields map[string]any, key string) string {
	v, ok := fields[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
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

// storeContainmentResolver answers, per project, whether provider writes are
// confined — reading the same stored config layers the binding resolver does.
//
// This is the wire that makes vconfig.ContainProviderWrites mean anything. The
// key parsed and NOTHING consulted it, so an operator who set
// contain_provider_writes = true got no containment and no indication the
// setting did nothing: a config that lies, which is worse than one that is
// absent because it is trusted.
//
// FAILS CLOSED-TO-OFF on a config error, matching the key's own contract:
// absent or malformed means OFF, because a boundary switched on by accident
// makes provider writes fail far from any visible cause.
//
// That reasoning INVERTED when the default flipped: a resolution failure now
// keeps containment ON, because failing open would silently drop the boundary
// precisely when config reads are already failing. See the body.
func storeContainmentResolver(st *store.Store) orch.ContainmentResolver {
	return func(ctx context.Context, projectID string) bool {
		// A resolution FAILURE keeps containment ON, and this direction inverted
		// with the default. While absent meant off, failing to read config could
		// not enable a boundary the operator never asked for. Now that absent
		// means on, returning false here would let a transient SQLite read error
		// silently launch an otherwise-admitted turn UNCONFINED -- reversing the
		// fail-closed default at exactly the moment the system is least healthy,
		// and with no signal, since the turn then succeeds normally.
		//
		// Failing closed costs a refused dispatch on an incapable binding, which
		// is visible and recoverable. Failing open costs a security boundary
		// nobody knows is missing.
		userCfg, err := vconfig.ResolveUser(ctx, st, "", "")
		if err != nil {
			return true
		}
		projectCfg, err := vconfig.ResolveProjects(ctx, st, userCfg, projectID)
		if err != nil {
			return true
		}
		return vconfig.ContainProviderWrites(projectCfg)
	}
}
