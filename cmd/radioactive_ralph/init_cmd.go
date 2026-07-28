package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/supervisor"
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

	// The supervisor is the SINGLE writer of record. init used to open the
	// store directly, which made the client a second writer to a
	// supervisor-owned database — the exact ownership split the
	// one-binary/supervisor-owned-state architecture exists to prevent, and the
	// last such caller in the CLI.
	//
	// Project resolution reuses ensureProjectKnown rather than repeating the
	// fingerprint/ProjectEnsure dance: one implementation means init and every
	// other command agree on what "this directory's project" is.
	projectID, err := ensureProjectKnown(ctx, cmd, stateRoot, cwd)
	if err != nil {
		return err
	}

	client, err := supervisor.Find(stateRoot)
	if err != nil {
		return fmt.Errorf(
			"%w: init needs a running supervisor; start one with: %s",
			errNoSupervisorListening, supervisorStartHint())
	}
	defer func() { _ = client.Close() }()

	fmt.Printf("radioactive_ralph: initialized project %s (%s)\n", projectID, cwd)

	return validateProjectConfig(ctx, cmd, newSupervisorConfigSource(stateRoot), projectID)
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
func validateProjectConfig(ctx context.Context, cmd *cobra.Command, src vconfig.ConfigSource, projectID string) error {
	configFile, userConfigFile, projectConfigFile := vconfig.FlagsFrom(cmd)

	userCfg, err := vconfig.ResolveUserFrom(ctx, src, configFile, userConfigFile)
	if err != nil {
		return fmt.Errorf("resolve user config: %w", err)
	}
	projectsCfg, err := vconfig.ResolveProjectsFrom(ctx, src, userCfg, projectID)
	if err != nil {
		return fmt.Errorf("resolve project config: %w", err)
	}

	var effective vconfig.ProjectConfig
	var pendingUpserts map[string]string
	var pendingDeletes []string
	incomingSelectionFound := false
	if projectConfigFile == "" {
		effective, err = vconfig.EffectiveProjectFrom(ctx, src, projectsCfg, projectID, "", vconfig.ModeChange)
		if err != nil {
			return fmt.Errorf("resolve effective project config: %w", err)
		}
	} else {
		incoming, err := vconfig.LoadFileValues(projectConfigFile)
		if err != nil {
			return fmt.Errorf("load project-config-file %s: %w", projectConfigFile, err)
		}
		incoming, selectionFound, err := normalizeIncomingProviderSelection(incoming)
		if err != nil {
			return fmt.Errorf("validate project provider selection: %w", err)
		}
		incomingSelectionFound = selectionFound

		overlay := incoming
		// Provider selection is one replaceable logical value even though legacy
		// ingress used a differently named singular key. An explicit --init
		// selection always replaces the stored selection; generic config keys
		// retain the existing conflict/--force-override UX.
		conflictInput := incoming
		if selectionFound {
			conflictInput = copyConfigValues(incoming)
			delete(conflictInput, providersConfigKey)
		}
		if conflicts := vconfig.DiffConflicts(projectsCfg, conflictInput); len(conflicts) > 0 {
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

		base := projectsCfg
		if selectionFound {
			base.Values = copyConfigValues(projectsCfg.Values)
			delete(base.Values, providerConfigKey)
		}
		effective, err = vconfig.EffectiveProjectFromValuesFrom(ctx, src, base, projectID, overlay, vconfig.ModeOverride)
		if err != nil {
			return fmt.Errorf("resolve effective project config: %w", err)
		}

		encoded, err := encodeProjectConfigValues(overlay)
		if err != nil {
			return err
		}
		pendingUpserts = encoded
		if selectionFound {
			pendingDeletes = []string{providerConfigKey}
		}
	}

	if !incomingSelectionFound {
		if _, _, err := resolveProviderNamesFromUserConfigSource(ctx, src, userCfg, projectID); err != nil {
			return fmt.Errorf("validate effective provider selection: %w", err)
		}
	}

	var requiredKeys []string // none required yet; later phases append here
	if missing := vconfig.Validate(effective, requiredKeys); len(missing) > 0 {
		return fmt.Errorf("%s", vconfig.FormatMissing(missing))
	}
	if pendingUpserts != nil {
		if err := src.ApplyProjectConfigValues(ctx, projectID, pendingUpserts, pendingDeletes); err != nil {
			return fmt.Errorf("persist project config: %w", err)
		}
	}
	return nil
}

func copyConfigValues(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func encodeProjectConfigValues(values map[string]any) (map[string]string, error) {
	encoded := make(map[string]string, len(values))
	for key, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode project config %q: %w", key, err)
		}
		encoded[key] = string(raw)
	}
	return encoded, nil
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
