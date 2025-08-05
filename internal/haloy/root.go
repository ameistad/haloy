package haloy

import (
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "haloy",
		Short:         "haloy builds and runs Docker containers based on a YAML config",
		SilenceErrors: true,
		SilenceUsage:  true, // Don't show usage on error
	}

	cmd.AddCommand(
		CompletionCmd(),
		DeployAppCmd(),
		RollbackAppCmd(),
		RollbackTargetsCmd(),
		StopAppCmd(),
		StatusAppCmd(),
		ValidateAppConfigCmd(),
		SecretsCommand(),
		LogsCmd(),
	)

	return cmd
}
