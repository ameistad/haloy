package haloy

import (
	"github.com/ameistad/haloy/internal/config"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "haloy",
		Short: "haloy builds and runs Docker containers based on a YAML config",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			config.LoadEnvFiles() // load environment variables in .env for all commands.
		},
		SilenceErrors: true,
		SilenceUsage:  true, // Don't show usage on error
	}

	cmd.AddCommand(
		CompletionCmd(),
		DeployAppCmd(),
		RollbackAppCmd(),
		RollbackTargetsCmd(),
		ServerCmd(),
		StopAppCmd(),
		StatusAppCmd(),
		ValidateAppConfigCmd(),
		SecretsCommand(),
		LogsCmd(),
		VersionCmd(),
	)

	return cmd
}
