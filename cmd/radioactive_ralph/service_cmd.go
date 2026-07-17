package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/jbcom/radioactive-ralph/internal/service"
	"github.com/spf13/cobra"
)

// newServiceCmd wires internal/service's per-user auto-restart definition
// as `radioactive_ralph service install|uninstall|status`. Installing
// registers the platform-native service host (launchd/systemd/Windows SCM)
// to run `radioactive_ralph --supervisor` as a long-lived, auto-restarting
// background process, so the supervisor survives logout/reboot/crash
// without an operator remembering to relaunch it by hand.
func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "service",
		Short:        "Manage the per-user supervisor auto-restart service definition",
		SilenceUsage: true,
	}
	cmd.AddCommand(newServiceInstallCmd())
	cmd.AddCommand(newServiceUninstallCmd())
	cmd.AddCommand(newServiceStatusCmd())
	return cmd
}

func newServiceInstallCmd() *cobra.Command {
	var ralphBin string
	var envPairs []string
	cmd := &cobra.Command{
		Use:          "install",
		Short:        "Install the supervisor as a per-user auto-restarting service",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
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
			path, err := service.Install(service.InstallOptions{RalphBin: bin, ExtraEnv: extraEnv})
			if err != nil {
				return fmt.Errorf("install service: %w", err)
			}
			fmt.Printf("radioactive_ralph: installed supervisor service definition at %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&ralphBin, "bin", "", "path to the radioactive_ralph binary the service should exec (default: this process's own executable path)")
	cmd.Flags().StringArrayVar(&envPairs, "env", nil, "extra KEY=VALUE environment variable for the service unit (repeatable)")
	return cmd
}

// parseEnvPairs parses repeated --env KEY=VALUE flag values into a map.
func parseEnvPairs(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
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

func newServiceUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "uninstall",
		Short:        "Remove the per-user supervisor service definition",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := service.Uninstall(service.InstallOptions{}); err != nil {
				return fmt.Errorf("uninstall service: %w", err)
			}
			fmt.Println("radioactive_ralph: supervisor service definition removed")
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
