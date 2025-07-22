package haloy

import (
	"context"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func DeployAppCmd() *cobra.Command {
	var configPath string
	var serverURL string
	var noLogs bool

	cmd := &cobra.Command{
		Use:   "deploy [config-path]",
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

			appConfig, err := config.LoadAndValidateAppConfig(configPath)
			if err != nil {
				ui.Error("Failed to load config: %v", err)
				return
			}

			targetServer := appConfig.Server
			if serverURL != "" {
				targetServer = serverURL
			}

			ui.Info("Starting deployment for application: %s using server %s", appConfig.Name, targetServer)
			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			apiClient := NewAPIClient(targetServer)
			resp, err := apiClient.Deploy(ctx, appConfig)
			if err != nil {
				ui.Error("Deployment request failed: %v", err)
				return
			}
			if resp == nil {
				ui.Error("No response from server")
				return
			}

			if !noLogs {
				logStreamer := NewLogStreamer(targetServer)
				if err := logStreamer.StreamLogs(ctx, "deploy", resp.DeploymentID); err != nil {
					ui.Warn("Failed to stream logs: %v", err)
				}
			}
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file or directory")
	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream deployment logs")

	return cmd
}
