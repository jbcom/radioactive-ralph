// Package vconfig implements the virtual-layer config resolution engine
// described in docs/superpowers/specs/2026-07-16-supervisor-architecture-design.md
// §5a. Configuration is never a single committed file: it resolves through
// two virtual layers built by the supervisor at runtime.
//
// Virtual USER config (low -> high precedence):
//
//	DB user-scope config < --config-file < --user-config-file
//
// Virtual PROJECTS config (per project):
//
//	all DB project config < projects: stanza from the virtual USER config
//
// viper does the mechanical defaults<file merge (TOML) per layer; this
// package owns the DB layer, the two-layer USER->PROJECTS composition, the
// projects: stanza extraction, the change-vs-override distinction (see
// effective.go), and conflict diffing (see diff.go).
package vconfig

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/spf13/viper"
)

// Fingerprint is a re-export of store.Fingerprint so callers of vconfig
// never need to import internal/store just to build fingerprints for
// UserScopeProjectID.
type Fingerprint = store.Fingerprint

// Mode distinguishes a persisted config CHANGE from a runtime-only
// OVERRIDE. See spec §5a "change vs. override — the load-bearing
// distinction".
type Mode int

const (
	// ModeChange persists merged project-config keys back to the store.
	// Used for the headless/TUI init wizard and an explicit --init.
	ModeChange Mode = iota

	// ModeOverride merges config on top of the effective project config
	// at runtime only; nothing is written to the store. Used for normal
	// (non-init) client runs that pass --project-config-file.
	ModeOverride
)

// String renders the mode name for logs/errors.
func (m Mode) String() string {
	switch m {
	case ModeChange:
		return "change"
	case ModeOverride:
		return "override"
	default:
		return fmt.Sprintf("Mode(%d)", int(m))
	}
}

// userScopeProjectDisplayName is the projects.display_name seeded for the
// reserved user-scope row. It exists purely so operators inspecting the DB
// directly can tell what the row is for; it carries no other meaning.
const userScopeProjectDisplayName = "__vconfig_user_scope__"

// userScopeFingerprintValue is the abs_path fingerprint value used to
// identify (and, on first use, create) the reserved project row that backs
// USER-level DB config. project_config.project_id has a NOT NULL foreign
// key onto projects(id), so user-scope values cannot be stored under a bare
// sentinel string like "__user__" — they need a real projects row. Using a
// fingerprint that can never collide with a real filesystem path (it is not
// an absolute path) keeps ResolveProject's lookup semantics: any repo whose
// abs_path fingerprint happens to be exactly this string would be a bug
// worth finding, not a silent collision.
const userScopeFingerprintValue = "vconfig://user-scope"

// UserScopeProjectID returns the reserved store project id that backs
// USER-level (as opposed to per-project) DB-resident config, creating the
// backing projects row on first use. It is idempotent: subsequent calls
// resolve the same row via its fingerprint rather than creating duplicates.
func UserScopeProjectID(ctx context.Context, st *store.Store) (string, error) {
	fps := []Fingerprint{{Kind: store.FingerprintKindAbsPath, Value: userScopeFingerprintValue}}
	if id, found, err := st.ResolveProject(ctx, fps); err != nil {
		return "", fmt.Errorf("vconfig: resolve user-scope project: %w", err)
	} else if found {
		return id, nil
	}
	id, err := st.CreateProject(ctx, userScopeProjectDisplayName, fps)
	if err != nil {
		return "", fmt.Errorf("vconfig: create user-scope project: %w", err)
	}
	return id, nil
}

// UserConfig is the resolved virtual USER layer: top-level values merged
// from DB user-scope config < --config-file < --user-config-file, plus the
// projects: stanza extracted from that same merge (also present at any of
// those three levels), keyed by the project's resolved store id.
type UserConfig struct {
	Values   map[string]any
	Projects map[string]map[string]any
}

// ProjectConfig is a resolved virtual PROJECTS layer for one project: DB
// project config overlaid by the projects: stanza entry for that project
// (ResolveProjects), and optionally further overlaid by a
// --project-config-file (EffectiveProject).
type ProjectConfig struct {
	Values map[string]any
}

// projectsStanzaKey is the top-level TOML key viper looks for the
// per-project overrides table under, e.g.:
//
//	[projects.<project-id>]
//	model = "opus"
const projectsStanzaKey = "projects"

// ResolveUser builds the virtual USER config: it seeds viper with defaults,
// merges in DB user-scope config (lowest precedence above defaults), then
// configFile, then userConfigFile (highest precedence). Both file paths are
// optional; an empty string skips that layer. Config files are TOML.
func ResolveUser(ctx context.Context, st *store.Store, configFile, userConfigFile string) (UserConfig, error) {
	v := viper.New()
	v.SetConfigType("toml")

	if st != nil {
		userProjectID, err := UserScopeProjectID(ctx, st)
		if err != nil {
			return UserConfig{}, err
		}
		dbValues, err := loadStoreConfig(ctx, st, userProjectID)
		if err != nil {
			return UserConfig{}, fmt.Errorf("vconfig: load DB user config: %w", err)
		}
		for k, val := range dbValues {
			v.SetDefault(k, val)
		}
	}

	if configFile != "" {
		if err := mergeFileInto(v, configFile); err != nil {
			return UserConfig{}, fmt.Errorf("vconfig: merge config-file %s: %w", configFile, err)
		}
	}
	if userConfigFile != "" {
		if err := mergeFileInto(v, userConfigFile); err != nil {
			return UserConfig{}, fmt.Errorf("vconfig: merge user-config-file %s: %w", userConfigFile, err)
		}
	}

	all := v.AllSettings()
	projects := extractProjectsStanza(all)
	delete(all, projectsStanzaKey)

	return UserConfig{Values: all, Projects: projects}, nil
}

// ResolveProjects builds the virtual PROJECTS config for one project: DB
// project config as the base, overlaid by userCfg.Projects[projectID] (the
// projects: stanza entry for that project, if any).
func ResolveProjects(ctx context.Context, st *store.Store, userCfg UserConfig, projectID string) (ProjectConfig, error) {
	base, err := loadStoreConfig(ctx, st, projectID)
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("vconfig: load DB project config: %w", err)
	}

	merged := cloneMap(base)
	if overlay, ok := userCfg.Projects[projectID]; ok {
		mergeMapInto(merged, overlay)
	}

	return ProjectConfig{Values: merged}, nil
}

// loadStoreConfig reads a project's DB-resident config and JSON-decodes
// each value back into a Go value (store persists values as JSON-encoded
// strings; see store.SetProjectConfig).
func loadStoreConfig(ctx context.Context, st *store.Store, projectID string) (map[string]any, error) {
	raw, err := st.GetProjectConfig(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		var decoded any
		if err := json.Unmarshal([]byte(v), &decoded); err != nil {
			// A non-JSON legacy value would be a store-layer bug, not a
			// vconfig caller error; surface it rather than silently
			// dropping the key.
			return nil, fmt.Errorf("vconfig: decode stored value for %q: %w", k, err)
		}
		out[k] = decoded
	}
	return out, nil
}

// mergeFileInto points v at path and merges it in. viper.MergeInConfig
// merges onto whatever is already in v (defaults + any prior file), giving
// exactly the precedence chain ResolveUser and EffectiveProject need.
func mergeFileInto(v *viper.Viper, path string) error {
	v.SetConfigFile(path)
	return v.MergeInConfig()
}

// extractProjectsStanza pulls the "projects" top-level table out of a
// viper AllSettings() map and normalizes it to map[string]map[string]any.
// viper lower-cases keys and may hand back nested maps as
// map[string]interface{}; TOML project ids are opaque strings so no further
// normalization is needed.
func extractProjectsStanza(all map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	raw, ok := all[projectsStanzaKey]
	if !ok {
		return out
	}
	rawMap, ok := raw.(map[string]any)
	if !ok {
		return out
	}
	for projectID, v := range rawMap {
		if sub, ok := v.(map[string]any); ok {
			out[projectID] = sub
		}
	}
	return out
}

// cloneMap returns a shallow copy of m (nil-safe: nil in, empty map out).
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// mergeMapInto copies every key of src into dst, overwriting existing keys.
func mergeMapInto(dst, src map[string]any) {
	for k, v := range src {
		dst[k] = v
	}
}
