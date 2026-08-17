package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
	"github.com/spf13/cobra"
)

func newAdaptersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adapters",
		Short: "Build reviewed Claude, Codex, and OpenCode enforcement bundles",
	}
	var target string
	install := &cobra.Command{
		Use:   "install",
		Short: "Atomically install a bundle without changing provider configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve current executable: %w", err)
			}
			manifest, err := adapters.Install(executable, target)
			if err != nil {
				return err
			}
			if err := adapters.ActivateTarget(target); err != nil {
				return err
			}
			encoded, err := json.Marshal(manifest)
			if err != nil {
				return fmt.Errorf("encode adapter manifest: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			return err
		},
	}
	install.Flags().StringVar(&target, "target", "", "absolute or relative bundle directory")
	_ = install.MarkFlagRequired("target")
	cmd.AddCommand(install)
	return cmd
}
