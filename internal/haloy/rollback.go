package haloy

import (
	"context"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func RollbackAppCmd() *cobra.Command {
	var configPath string
	var noLogs bool

	cmd := &cobra.Command{
		Use:   "rollback [path] <deployment-id>",
		Short: "Rollback an application to a specific deployment",
		Long: `Rollback an application to a specific deployment using a haloy configuration file.

The path can be:
- A directory containing haloy.json, haloy.yaml, haloy.yml, or haloy.toml
- A full path to a config file with supported extension
- A relative path to either of the above

If no path is provided, the current directory is used.

Use 'haloy rollback-targets [path]' to list available deployment IDs.`,
		Args: cobra.RangeArgs(1, 2), // 1-2 args: [path] deployment-id OR deployment-id
		Run: func(cmd *cobra.Command, args []string) {
			var targetDeploymentID string

			// Parse arguments - handle both patterns:
			// rollback <deployment-id>
			// rollback <path> <deployment-id>
			if len(args) == 1 {
				// rollback <deployment-id>
				targetDeploymentID = args[0]
				if configPath == "" {
					configPath = "."
				}
			} else {
				// rollback <path> <deployment-id>
				configPath = args[0]
				targetDeploymentID = args[1]
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
			resp, err := apiClient.Rollback(ctx, appConfig.Name, targetDeploymentID)
			if err != nil {
				ui.Error("Rollback failed: %v", err)
				return
			}

			ui.Success("🔄 Rollback initiated: %s", resp.Message)
			ui.Info("📋 New deployment ID: %s", resp.DeploymentID)

			if !noLogs {
				logStreamer := NewLogStreamer(appConfig.Server)
				if err := logStreamer.StreamLogs(ctx, "deploy", resp.DeploymentID); err != nil {
					ui.Warn("Failed to stream logs: %v", err)
				}
			}
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to haloy config file or directory")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream deployment logs")

	return cmd
}

func RollbackTargetsCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "rollback-targets [path]",
		Short: "List available rollback targets for an application",
		Long: `List available rollback targets for an application using a haloy configuration file.

The path can be:
- A directory containing haloy.json, haloy.yaml, haloy.yml, or haloy.toml
- A full path to a config file with supported extension
- A relative path to either of the above

If no path is provided, the current directory is used.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// Determine config path (same logic as other commands)
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

			// Get rollback targets via API
			targets, err := apiClient.RollbackTargets(ctx, appConfig.Name)
			if err != nil {
				ui.Error("Failed to get rollback targets: %v", err)
				return
			}

			if len(targets.Targets) == 0 {
				ui.Info("No rollback targets available for app '%s'", appConfig.Name)
				return
			}

			displayRollbackTargets(appConfig.Name, targets.Targets, configPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to haloy config file or directory")
	return cmd
}

func displayRollbackTargets(appName string, targets []deploy.RollbackTarget, configPath string) {
	if len(targets) == 0 {
		ui.Info("No rollback targets available for app '%s'", appName)
		return
	}
	ui.Info("📋 Available rollback targets for '%s':", appName)
	ui.Info("")

	headers := []string{"DEPLOYMENT ID", "IMAGE TAG", "DATE"}
	rows := make([][]string, 0, len(targets))

	for _, target := range targets {

		date := "N/A"
		if formattedDate, err := helpers.FormatDateString(target.DeploymentID); err == nil {
			date = formattedDate
		}

		rows = append(rows, []string{
			target.DeploymentID,
			target.ImageTag,
			date,
		})
	}

	ui.Table(headers, rows)
	ui.Basic("To rollback, run:")
	if configPath == "." {
		ui.Basic("  haloy rollback <deployment-id>")
	} else {
		ui.Basic("  haloy rollback %s <deployment-id>", configPath)
	}
}
