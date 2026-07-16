package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/store"
	"github.com/jbcom/radioactive-ralph/internal/vconfig"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/spf13/cobra"
)

// flagForceOverride is the --init-only escape hatch for the DiffConflicts
// check in validateProjectConfig: see that function's doc comment for the
// UX this flag controls.
const flagForceOverride = "force-override"

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
// so the Validate check below is currently a clean no-op path that
// exercises the real vconfig/store wiring end-to-end rather than a
// placeholder.
//
// Before persisting, an incoming --project-config-file is diffed against
// the project's ALREADY-STORED config via vconfig.DiffConflicts (--init
// always runs in vconfig.ModeChange, so EffectiveProject would otherwise
// silently persist an override with no operator visibility at all). The
// chosen UX, since --init is headless (no prompt to answer y/N against):
//
//   - No conflicts: proceed exactly as before, merge + persist.
//   - Conflicts, --force-override NOT passed (the default): AUTO-REMOVE the
//     conflicting keys from what gets applied — vconfig.AutoRemove — and
//     print a clear notice naming every dropped key with its stored vs.
//     incoming value. The run still succeeds and every NON-conflicting
//     incoming key is still applied; only the disputed keys are skipped.
//     This keeps a headless/CI --init run from hard-failing on a config
//     drift it didn't cause, while never silently discarding data the
//     operator can't see was discarded.
//   - Conflicts, --force-override passed: apply the incoming values
//     VERBATIM (no auto-remove), logging exactly which keys were
//     overridden and with what, so there is still a durable record even
//     though the operator has opted into overriding.
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

	var effective vconfig.ProjectConfig
	if projectConfigFile == "" {
		effective, err = vconfig.EffectiveProject(ctx, st, projectsCfg, projectID, "", vconfig.ModeChange)
		if err != nil {
			return fmt.Errorf("resolve effective project config: %w", err)
		}
	} else {
		incoming, err := vconfig.LoadFileValues(projectConfigFile)
		if err != nil {
			return fmt.Errorf("load project-config-file %s: %w", projectConfigFile, err)
		}

		overlay := incoming
		if conflicts := vconfig.DiffConflicts(projectsCfg, incoming); len(conflicts) > 0 {
			forceOverride, _ := cmd.Flags().GetBool(flagForceOverride)
			if forceOverride {
				fmt.Printf("radioactive_ralph: --force-override applying %d conflicting key(s) from %s:\n%s",
					len(conflicts), projectConfigFile, formatConflicts(conflicts))
			} else {
				overlay = vconfig.AutoRemove(incoming, conflicts)
				fmt.Printf("radioactive_ralph: %d key(s) from %s would override existing project config and were SKIPPED (rerun with --force-override to apply them anyway):\n%s",
					len(conflicts), projectConfigFile, formatConflicts(conflicts))
			}
		}

		effective, err = vconfig.EffectiveProjectFromValues(ctx, st, projectsCfg, projectID, overlay, vconfig.ModeChange)
		if err != nil {
			return fmt.Errorf("resolve effective project config: %w", err)
		}
	}

	var requiredKeys []string // none required yet; later phases append here
	if missing := vconfig.Validate(effective, requiredKeys); len(missing) > 0 {
		return fmt.Errorf("%s", vconfig.FormatMissing(missing))
	}
	return nil
}

// formatConflicts renders one line per conflict, sorted by key for stable
// output, as "  key: stored -> incoming".
func formatConflicts(conflicts []vconfig.Conflict) string {
	sorted := make([]vconfig.Conflict, len(conflicts))
	copy(sorted, conflicts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

	var b strings.Builder
	for _, c := range sorted {
		fmt.Fprintf(&b, "  %s: %v -> %v\n", c.Key, c.Stored, c.Incoming)
	}
	return b.String()
}
