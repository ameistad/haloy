package haloy

import (
	"github.com/ameistad/haloy/internal/config"
	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	var configPathFlag string
	resolvedConfigPath := "."

	cmd := &cobra.Command{
		Use:   "haloy",
		Short: "haloy builds and runs Docker containers based on a YAML config",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			config.LoadEnvFiles() // load environment variables in .env for all commands.

			if cmd.Name() == "completion" || cmd.Parent().Name() == "server" {
				return
			}

			if configPathFlag != "" {
				resolvedConfigPath = configPathFlag
			}
		},
		SilenceErrors: true,
		SilenceUsage:  true, // Don't show usage on error
	}

	cmd.AddCommand(
		CompletionCmd(),
		ServerCmd(),
	)

	// Add resolvedConfigPath
	appCommands := []*cobra.Command{
		DeployAppCmd(&resolvedConfigPath),
		LogsCmd(&resolvedConfigPath),
		RollbackAppCmd(&resolvedConfigPath),
		RollbackTargetsCmd(&resolvedConfigPath),
		SecretsCmd(&resolvedConfigPath),
		StatusAppCmd(&resolvedConfigPath),
		ValidateAppConfigCmd(&resolvedConfigPath),
		StopAppCmd(&resolvedConfigPath),
		VersionCmd(&resolvedConfigPath),
	}

	for _, appCmd := range appCommands {
		addAppConfigFlag(appCmd, &configPathFlag) // Apply the shared flag
		cmd.AddCommand(appCmd)
	}

	return cmd
}

func addAppConfigFlag(cmd *cobra.Command, configPathFlag *string) {
	cmd.Flags().StringVarP(configPathFlag, "config", "c", "", "Path to config file or directory (default: .)")
}
