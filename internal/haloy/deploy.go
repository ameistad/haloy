package haloy

import (
	"context"
	"time"

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

			ui.Info("📁 Loading configuration from: %s", configPath)
			appConfig, err := config.LoadAndValidateAppConfig(configPath)
			if err != nil {
				ui.Error("Failed to load config: %v", err)
				return
			}

			ui.Info("🌐 Using server: %s", appConfig.Server)
			ctx, cancel := context.WithTimeout(context.Background(), deploy.DefaultContextTimeout)
			defer cancel()

			apiClient := NewAPIClient(appConfig.Server)
			logStreamer := NewLogStreamer(appConfig.Server)
			resp, err := apiClient.Deploy(ctx, appConfig)
			if err != nil {
				ui.Error("Deployment request failed: %v", err)
				return
			}
			if resp == nil {
				ui.Error("No response from server")
				return
			}

			if resp.Message != "" {
				ui.Info("%s", resp.Message)
			}

			// Wait before connecting to log stream.
			time.Sleep(1 * time.Second)

			logCtx, logCancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer logCancel()

			if err := logStreamer.StreamLogs(logCtx, "deploy", resp.DeploymentID); err != nil {
				ui.Warn("Failed to stream logs from API: %v", err)
				ui.Info("You can check operation status manually")
			}
		},
	}

	deployAppCmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file or directory")
	deployAppCmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	deployAppCmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream deployment logs")

	return deployAppCmd
}
