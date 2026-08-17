package main

import (
	"os"

	"github.com/jbcom/radioactive-ralph/internal/adapters"
	"github.com/spf13/cobra"
)

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Provider hook ingress for generated enforcement adapters",
		Hidden: true,
	}
	var adapter, event string
	eventCmd := &cobra.Command{
		Use:          "event",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return adapters.RunHook(
				cmd.Context(), adapter, event,
				cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), os.Getenv,
			)
		},
	}
	eventCmd.Flags().StringVar(&adapter, "adapter", "", "provider adapter name")
	eventCmd.Flags().StringVar(&event, "event", "", "normalized hook event")
	_ = eventCmd.MarkFlagRequired("adapter")
	_ = eventCmd.MarkFlagRequired("event")
	cmd.AddCommand(eventCmd)
	return cmd
}
