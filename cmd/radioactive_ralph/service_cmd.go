package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jbcom/radioactive-ralph/internal/service"
	"github.com/jbcom/radioactive-ralph/internal/xdg"
	"github.com/spf13/cobra"
)

var startSupervisorService = service.Start
var stopSupervisorService = service.Stop
var waitSupervisorServiceReady = waitSupervisorReachable

// newServiceCmd wires internal/service's per-user auto-restart definition
// as `radioactive_ralph service install|uninstall|status`. Installing
// registers the supported platform-native service host (launchd/systemd)
// to run `radioactive_ralph --supervisor` as a long-lived, auto-restarting
// background process, so the supervisor survives logout/reboot/crash
// without an operator remembering to relaunch it by hand. Native Windows SCM
// install/start is fail-closed; status/uninstall remain for remediation.
func newServiceCmd() *cobra.Command {
	return newServiceCmdForPlatform(runtime.GOOS)
}

func newServiceCmdForPlatform(goos string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "service",
		Short:        "Manage the per-user supervisor auto-restart service definition",
		SilenceUsage: true,
	}
	cmd.AddCommand(newServiceInstallCmd())
	cmd.AddCommand(newServiceUninstallCmd())
	cmd.AddCommand(newServiceStatusCmd())
	applyServiceHelpForPlatform(cmd, goos)
	return cmd
}

func applyServiceHelpForPlatform(cmd *cobra.Command, goos string) {
	if goos != "windows" {
		return
	}

	cmd.Short = "Inspect or remove legacy Windows SCM state; install/start is unsupported"
	cmd.Long = "Native Windows SCM install/start is unsupported. Run `radioactive_ralph --supervisor` as a foreground control plane and `radioactive_ralph` as its client. The status and uninstall commands exist only to inspect and remove legacy SCM registrations. Use WSL2 for the functional per-user service and provider-backed execution path."

	help := map[string]struct {
		short string
		long  string
	}{
		"install": {
			short: "Unsupported on native Windows; use the foreground control plane or WSL2",
			long:  "Native Windows SCM install/start is unsupported. Run `radioactive_ralph --supervisor` in a foreground terminal and `radioactive_ralph` as its client. Use WSL2 for the functional Linux per-user service and provider-backed execution path.",
		},
		"uninstall": {
			short: "Stop and remove a legacy native Windows SCM registration",
			long:  "Native Windows uninstall is a remediation command: it stops and removes a legacy SCM registration. It does not enable a supported native service path. Use WSL2 for the functional per-user service and provider-backed execution path.",
		},
		"status": {
			short: "Inspect a legacy native Windows SCM registration for remediation",
			long:  "Native Windows status reports legacy SCM registration state for remediation. Native SCM install/start remains unsupported; run the control plane in the foreground or use WSL2 for the functional per-user service and provider-backed execution path.",
		},
	}
	for _, child := range cmd.Commands() {
		if guidance, ok := help[child.Name()]; ok {
			child.Short = guidance.short
			child.Long = guidance.long
		}
	}
}

func newServiceInstallCmd() *cobra.Command {
	var ralphBin string
	var envPairs []string
	var inheritShellEnv bool
	cmd := &cobra.Command{
		Use:          "install",
		Short:        "Install the supervisor as a per-user auto-restarting service",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bin := ralphBin
			if bin == "" {
				exe, err := os.Executable()
				if err != nil {
					return fmt.Errorf("resolve own executable path: %w", err)
				}
				bin = exe
			}
			extraEnv, err := parseEnvPairs(envPairs)
			if err != nil {
				return err
			}
			if inheritShellEnv {
				shellEnv, err := captureLoginShellEnv()
				if err != nil {
					return fmt.Errorf("capture login shell environment: %w", err)
				}
				// Shell env is the BASE; explicit --env overrides win on top.
				for k, v := range shellEnv {
					if _, explicit := extraEnv[k]; !explicit {
						extraEnv[k] = v
					}
				}
			}
			if _, configured := extraEnv[maxParallelEnv]; configured {
				if _, err := supervisorMaxParallel(func(key string) (string, bool) {
					value, ok := extraEnv[key]
					return value, ok
				}); err != nil {
					return fmt.Errorf("validate service environment: %w", err)
				}
			}
			if _, explicitlySet := extraEnv["PATH"]; !explicitlySet {
				if inferredPath := serviceExecutionPath(bin, os.Getenv("PATH")); inferredPath != "" {
					extraEnv["PATH"] = inferredPath
				}
			}
			if _, explicitlySet := extraEnv["HOME"]; !explicitlySet {
				if home, err := os.UserHomeDir(); err == nil {
					extraEnv["HOME"] = home
				}
			}
			opts := service.InstallOptions{RalphBin: bin, ExtraEnv: extraEnv}
			path, err := service.Install(opts)
			if err != nil {
				return fmt.Errorf("install service: %w", err)
			}
			if err := startSupervisorService(opts); err != nil {
				return fmt.Errorf("start installed service: %w", err)
			}
			stateRoot := strings.TrimSpace(extraEnv["RALPH_STATE_DIR"])
			if stateRoot == "" {
				stateRoot, err = xdg.StateRoot()
				if err != nil {
					return fmt.Errorf("resolve service state root: %w", err)
				}
			}
			if !waitSupervisorServiceReady(cmd.Context(), stateRoot, 10*time.Second) {
				return fmt.Errorf("service manager accepted the start, but the supervisor endpoint at %s did not become ready", stateRoot)
			}
			fmt.Printf("radioactive_ralph: installed and started supervisor service at %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&ralphBin, "bin", "", "path to the radioactive_ralph binary the service should exec (default: this process's own executable path)")
	cmd.Flags().StringArrayVar(&envPairs, "env", nil, "extra KEY=VALUE environment variable for the service unit (repeatable)")
	cmd.Flags().BoolVar(&inheritShellEnv, "inherit-shell-env", false, "capture the user's login shell environment and bake it into the service unit, so provider CLIs (claude, codex, opencode, agy) inherit the same HOME, PATH, keyring access, and env vars they'd get in a terminal")
	return cmd
}

// serviceExecutionPath persists a safe, absolute-only subset of the
// operator's current PATH into the user service. launchd starts agents with
// only /usr/bin:/bin:/usr/sbin:/sbin, which cannot find Homebrew or ~/.local
// provider CLIs; systemd user-manager PATHs have the same shell-vs-service
// drift. The Ralph binary's directory is considered first. Duplicate,
// relative, missing, and non-directory entries are removed. Unix additionally
// rejects paths with symlinked or ownership/mode-untrusted components. Native
// Windows SCM installation is disabled, so no installer PATH is inferred.
func serviceExecutionPath(ralphBin, current string) string {
	if runtime.GOOS == "windows" {
		// Keep this helper reductive even though service.Install rejects the
		// operation: no caller may convert the installer's PATH into dormant
		// SCM configuration.
		return ""
	}

	currentEntries := filepath.SplitList(current)
	candidates := make([]string, 0, 1+len(currentEntries)+6)
	candidates = append(candidates, filepath.Dir(ralphBin))
	candidates = append(candidates, currentEntries...)
	candidates = append(candidates,
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	)

	seen := make(map[string]struct{}, len(candidates))
	paths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		clean := filepath.Clean(strings.TrimSpace(candidate))
		if clean == "." || !filepath.IsAbs(clean) {
			continue
		}
		if !servicePathDirAllowed(clean) {
			continue
		}
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		paths = append(paths, clean)
	}
	return strings.Join(paths, string(os.PathListSeparator))
}

// parseEnvPairs parses repeated --env KEY=VALUE flag values into a map.
func parseEnvPairs(pairs []string) (map[string]string, error) {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --env value %q: want KEY=VALUE", p)
		}
		out[k] = v
	}
	return out, nil
}

// captureLoginShellEnv runs the user's login shell non-interactively to
// capture the full environment a provider CLI would see in a terminal. This
// is what makes `--inherit-shell-env` work: launchd starts agents with a
// minimal environment (no HOME unless we set it, no PATH to Homebrew, no
// keychain access context, no env vars from .zshrc/.bash_profile). Provider
// CLIs — claude, codex, opencode, agy — all store auth credentials under
// $HOME or in the macOS keychain, and several read config from env vars set
// in the user's shell profile. Without the login environment, they can't
// find their credentials and fail with opaque auth errors.
//
// The captured env is the BASE layer; explicit --env flags override on top,
// so an operator can still pin a specific PATH or HOME while inheriting
// everything else from the shell.
//
// A controlled set of variables is excluded: shell-internal state (SHLVL,
// _), session-specific values (OLDPWD, PWD), and variables that are
// meaningless to a background service (TERM, SSH_*, DISPLAY, WINDOWID).
func captureLoginShellEnv() (map[string]string, error) {
	output, err := captureLoginShellEnvRaw()
	if err != nil {
		return nil, err
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || k == "" {
			continue
		}
		if shouldExcludeFromShellEnv(k) {
			continue
		}
		env[k] = v
	}
	return env, nil
}

// shellEnvExclude lists variables that are shell-internal, session-specific,
// or meaningless to a background service. Excluding them keeps the service
// unit clean and avoids overriding values launchd/systemd already set
// correctly (like the service's own PWD).
var shellEnvExclude = map[string]bool{
	"SHLVL": true, "_": true, "OLDPWD": true, "PWD": true,
	"TERM": true, "TERM_SESSION_ID": true,
	"SSH_AUTH_SOCK": true, "SSH_CONNECTION": true,
	"DISPLAY": true, "WINDOWID": true, "ITERM_PROFILE": true,
	"ITERM_SESSION_ID": true, "__CFBundleIdentifier": true,
	"LSCOLORS": true, "FPATH": true,
	// launchd sets these itself; don't let the shell's values clobber them.
	"LAUNCHED_BY": true,
}

func shouldExcludeFromShellEnv(key string) bool {
	return shellEnvExclude[key]
}

func newServiceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "uninstall",
		Short:        "Remove the per-user supervisor service definition",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := stopSupervisorService(service.InstallOptions{}); err != nil {
				return fmt.Errorf("stop service before uninstall: %w", err)
			}
			if err := service.Uninstall(service.InstallOptions{}); err != nil {
				return fmt.Errorf("uninstall service: %w", err)
			}
			fmt.Println("radioactive_ralph: supervisor service stopped and definition removed")
			return nil
		},
	}
}

func newServiceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Short:        "Report whether the per-user supervisor service is installed",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			status, err := service.Inspect(service.InstallOptions{})
			if err != nil {
				return fmt.Errorf("inspect service: %w", err)
			}
			if status.Installed {
				fmt.Printf("radioactive_ralph: supervisor service installed (%s, %s)\n", status.Backend, status.UnitPath)
			} else {
				fmt.Printf("radioactive_ralph: supervisor service NOT installed (%s)\n", status.Backend)
			}
			return nil
		},
	}
}
