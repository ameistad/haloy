package haloyadm

import (
	"github.com/spf13/cobra"
)

// NewRootCmd creates the root command
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "haloyadm",
		Short:         "Commands to manage the haloy-manager",
		SilenceErrors: true, // Don't print errors automatically
		SilenceUsage:  true, // Don't show usage on error
	}

	// Add all subcommands
	cmd.AddCommand(
		InitCmd(),
		StartCmd(),
		StopCmd(),
		APICmd(),
		SecretsCmd(),
	)

	return cmd
}
