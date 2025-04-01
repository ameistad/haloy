package commands

import (
	"github.com/spf13/cobra"
)

// NewRootCmd creates the root command
func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "haloy",
		Short:         "haloy builds and runs Docker containers based on a YAML config",
		SilenceErrors: true, // Don't print errors automatically
		SilenceUsage:  true, // Don't show usage on error
	}

	// Add all subcommands
	cmd.AddCommand(
		CompletionCmd(),
		DeployAppCmd(),
		DeployAllCmd(),
		InitCmd(),
		ListAppsCmd(),
		RollbackAppCmd(),
		StatusAppCmd(),
		StatusAllCmd(),
		ValidateCmd(),
		VersionCmd(),
		SecretsCommand(),
	)

	return cmd
}
