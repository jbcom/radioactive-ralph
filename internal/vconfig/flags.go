package vconfig

import "github.com/spf13/cobra"

// Flag name constants, exported so callers wiring up cobra commands or
// reading flag values by name elsewhere don't need to hardcode strings.
const (
	FlagConfigFile        = "config-file"
	FlagConfigFileShort   = "C"
	FlagUserConfigFile    = "user-config-file"
	FlagProjectConfigFile = "project-config-file"
)

// AddFlags registers the three virtual-config-layer flags described in
// spec §5a as persistent flags on cmd:
//
//   - --config-file / -C     : joint config file; may carry a projects: stanza.
//   - --user-config-file     : user-specific config file; may also carry a
//     projects: stanza.
//   - --project-config-file  : config for one specific project.
//
// --project-config-file is IGNORED in --supervisor mode: the supervisor
// operates at the user/XDG level with the working directory irrelevant
// (spec §4), so there is no single "current project" for it to apply to.
// The supervisor entry point simply never reads this flag's value; callers
// building the supervisor command should not surface it in help text as
// meaningful there, but AddFlags registers it unconditionally so the same
// flag set can be shared between the supervisor and dumb-client commands.
func AddFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringP(FlagConfigFile, FlagConfigFileShort, "", "path to a joint config file (may contain a [projects.*] stanza)")
	cmd.PersistentFlags().String(FlagUserConfigFile, "", "path to a user-specific config file (may contain a [projects.*] stanza)")
	cmd.PersistentFlags().String(FlagProjectConfigFile, "", "path to a single-project config file (ignored in --supervisor mode)")
}

// FlagsFrom reads back the three virtual-config-layer flag values
// registered by AddFlags. Missing/unregistered flags resolve to "".
func FlagsFrom(cmd *cobra.Command) (configFile, userConfigFile, projectConfigFile string) {
	configFile, _ = cmd.Flags().GetString(FlagConfigFile)
	userConfigFile, _ = cmd.Flags().GetString(FlagUserConfigFile)
	projectConfigFile, _ = cmd.Flags().GetString(FlagProjectConfigFile)
	return configFile, userConfigFile, projectConfigFile
}
