package vconfig

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/spf13/viper"
)

// EffectiveProject computes the final, effective config for one project:
// projectsCfg (the virtual PROJECTS layer from ResolveProjects) optionally
// overlaid by projectConfigFile.
//
// projectConfigFile is optional; an empty string returns projectsCfg
// unchanged. When non-empty, its keys are always merged into the returned
// config's Values. The mode governs persistence, per spec §5a
// "change vs. override":
//
//   - ModeChange: the merged keys are ALSO persisted to the store via
//     st.SetProjectConfig, so this becomes the project's new baseline
//     (used by the init wizard / explicit --init).
//   - ModeOverride: runtime-only. Nothing is written to the store; the
//     project's stored initialization is left untouched.
//
// --project-config-file is ignored in --supervisor mode (the supervisor
// path simply never calls EffectiveProject with a projectConfigFile) — see
// spec §5a and the AddFlags doc comment.
func EffectiveProject(ctx context.Context, st *store.Store, projectsCfg ProjectConfig, projectID, projectConfigFile string, mode Mode) (ProjectConfig, error) {
	if projectConfigFile == "" {
		return projectsCfg, nil
	}

	overlay, err := loadFileValues(projectConfigFile)
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("vconfig: load project-config-file %s: %w", projectConfigFile, err)
	}

	merged := cloneMap(projectsCfg.Values)
	mergeMapInto(merged, overlay)

	if mode == ModeChange {
		if st == nil {
			return ProjectConfig{}, fmt.Errorf("vconfig: ModeChange requires a store")
		}
		for k, v := range overlay {
			encoded, err := json.Marshal(v)
			if err != nil {
				return ProjectConfig{}, fmt.Errorf("vconfig: encode %q for persist: %w", k, err)
			}
			if err := st.SetProjectConfig(ctx, projectID, k, string(encoded)); err != nil {
				return ProjectConfig{}, fmt.Errorf("vconfig: persist %q: %w", k, err)
			}
		}
	}

	return ProjectConfig{Values: merged}, nil
}

// loadFileValues loads a standalone TOML file (a --project-config-file) and
// returns its top-level settings as a flat map[string]any.
func loadFileValues(path string) (map[string]any, error) {
	v := viper.New()
	v.SetConfigType("toml")
	if err := mergeFileInto(v, path); err != nil {
		return nil, err
	}
	return v.AllSettings(), nil
}
