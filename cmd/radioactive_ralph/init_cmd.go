package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/vconfig"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/spf13/cobra"
)

// runInitMode implements the headless `--init` path described in spec §4:
// it ensures the current directory exists as a known project in the
// user-level store (creating it with accumulated fingerprints, §5b, if
// it's new) and validates the merged virtual config for that project
// (§5a). The full interactive wizard is a later phase; this is the
// flag-driven equivalent that a script or CI job can run non-interactively.
//
// --init always CHANGES the project's stored config (spec §5a: "CHANGES
// occur via the headless/TUI wizard or an explicit --init"), so a passed
// --project-config-file is merged in AND persisted, unlike the override
// semantics of a plain client run.
func runInitMode(ctx context.Context, cmd *cobra.Command) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve cwd: %w", err)
	}

	stateRoot, err := xdg.StateRoot()
	if err != nil {
		return fmt.Errorf("resolve state root: %w", err)
	}
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return fmt.Errorf("create state root: %w", err)
	}

	st, err := store.Open(ctx, store.Options{DSN: store.DSN(storeDBPath(stateRoot))})
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { _ = st.Close() }()

	fps, err := store.Fingerprints(ctx, cwd)
	if err != nil {
		return fmt.Errorf("compute project fingerprints: %w", err)
	}

	projectID, found, err := st.ResolveProject(ctx, fps)
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	if found {
		if err := st.AddProjectIdentifiers(ctx, projectID, fps); err != nil {
			return fmt.Errorf("accumulate project identifiers: %w", err)
		}
		if err := st.TouchProjectLastSeen(ctx, projectID); err != nil {
			return fmt.Errorf("touch project: %w", err)
		}
		fmt.Printf("radioactive_ralph: re-initialized existing project %s (%s)\n", projectID, cwd)
	} else {
		projectID, err = st.CreateProject(ctx, filepath.Base(cwd), fps)
		if err != nil {
			return fmt.Errorf("create project: %w", err)
		}
		fmt.Printf("radioactive_ralph: initialized new project %s (%s)\n", projectID, cwd)
	}

	return validateProjectConfig(ctx, cmd, st, projectID)
}

// validateProjectConfig resolves the virtual USER + PROJECTS config layers
// (spec §5a) for projectID and reports any missing required keys. Nothing
// is required yet at this phase (plan orchestration and provider binding
// resolution, which introduce real required keys, land in later phases),
// so this is currently a clean no-op path that exercises the real
// vconfig/store wiring end-to-end rather than a placeholder.
func validateProjectConfig(ctx context.Context, cmd *cobra.Command, st *store.Store, projectID string) error {
	configFile, userConfigFile, projectConfigFile := vconfig.FlagsFrom(cmd)

	userCfg, err := vconfig.ResolveUser(ctx, st, configFile, userConfigFile)
	if err != nil {
		return fmt.Errorf("resolve user config: %w", err)
	}
	projectsCfg, err := vconfig.ResolveProjects(ctx, st, userCfg, projectID)
	if err != nil {
		return fmt.Errorf("resolve project config: %w", err)
	}
	effective, err := vconfig.EffectiveProject(ctx, st, projectsCfg, projectID, projectConfigFile, vconfig.ModeChange)
	if err != nil {
		return fmt.Errorf("resolve effective project config: %w", err)
	}

	var requiredKeys []string // none required yet; later phases append here
	if missing := vconfig.Validate(effective, requiredKeys); len(missing) > 0 {
		return fmt.Errorf("%s", vconfig.FormatMissing(missing))
	}
	return nil
}
