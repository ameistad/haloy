package commands

import (
	"context"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func DeployAppCmd() *cobra.Command {
	var configPath string
	var serverURL string
	var noLogs bool

	deployAppCmd := &cobra.Command{
		Use:   "deploy [path]",
		Short: "Deploy an application",
		Long: `Deploy an application using a haloy configuration file.

The path can be:
- A directory containing haloy.json, haloy.yaml, haloy.yml, or haloy.toml
- A full path to a config file with supported extension (.json, .yaml, .yml, .toml)
- A relative path to either of the above

If no path is provided, the current directory is used.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// Determine config path
			if len(args) > 0 {
				configPath = args[0]
			} else if configPath == "" {
				configPath = "."
			}

			// Load and validate the config
			ui.Info("📁 Loading configuration from: %s", configPath)
			appConfig, err := config.LoadAndValidateAppConfig(configPath)
			if err != nil {
				ui.Error("Failed to load config: %v", err)
				return
			}

			ui.Success("Configuration loaded successfully for app: %s", appConfig.Name)

			ui.Info("🌐 Using server: %s", appConfig.Server)

			// Create command executor and execute deploy
			executor := NewCommandExecutor(appConfig.Server)

			ctx, cancel := context.WithTimeout(context.Background(), deploy.DefaultContextTimeout)
			defer cancel()

			if err := executor.ExecuteCommandWithLogs(ctx, "deploy", appConfig, !noLogs); err != nil {
				// Error already logged by executor
				return
			}
		},
	}

	deployAppCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file or directory")
	deployAppCmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	deployAppCmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream deployment logs")

	return deployAppCmd
}
