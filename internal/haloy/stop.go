package haloy

import (
	"context"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func StopAppCmd() *cobra.Command {
	var configPath string
	var serverURL string
	var removeContainersFlag bool

	cmd := &cobra.Command{
		Use:   "stop [config-path]",
		Short: "Stop an application's running containers",
		Long: `Stop all running containers for an application using a haloy configuration file.

The path can be:
- A directory containing haloy.json, haloy.yaml, haloy.yml, or haloy.toml
- A full path to a config file with supported extension
- A relative path to either of the above

If no path is provided, the current directory is used.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// Determine config path (consistent with other commands)
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

			// Same server logic as other commands
			targetServer := appConfig.Server
			if serverURL != "" {
				targetServer = serverURL
			}

			ui.Info("Stopping application: %s using server %s", appConfig.Name, targetServer)
			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			apiClient := NewAPIClient(targetServer)
			response, err := apiClient.StopApp(ctx, appConfig.Name, removeContainersFlag)
			if err != nil {
				ui.Error("Failed to stop app: %v", err)
				return
			}

			if len(response.StoppedIDs) > 0 {
				ui.Success("Successfully stopped %d container(s) for app '%s'", len(response.StoppedIDs), appConfig.Name)
			} else {
				ui.Info("No running containers found for app '%s'", appConfig.Name)
			}

			if removeContainersFlag && len(response.RemovedIDs) > 0 {
				ui.Success("Successfully removed %d container(s) for app '%s'", len(response.RemovedIDs), appConfig.Name)
			}
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file or directory")
	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().BoolVarP(&removeContainersFlag, "remove-containers", "r", false, "Remove containers after stopping them")

	return cmd
}
