package haloy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploytypes"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func RollbackAppCmd(configPath *string) *cobra.Command {
	var serverURL string
	var noLogs bool

	cmd := &cobra.Command{
		Use:   "rollback <deployment-id>",
		Short: "Rollback an application to a specific deployment",
		Long: `Rollback an application to a specific deployment using a haloy configuration file.

Use 'haloy rollback-targets' to list available deployment IDs.`,
		Args: cobra.RangeArgs(1, 2), // 1-2 args: [path] deployment-id OR deployment-id
		Run: func(cmd *cobra.Command, args []string) {
			var targetDeploymentID string

			appConfig, _, err := config.LoadAppConfig(*configPath)
			if err != nil {
				ui.Error("Failed to load config: %v", err)
				return
			}

			targetServer, err := getServer(appConfig, serverURL)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			token, err := getToken(appConfig, targetServer)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			newDeploymentID := createDeploymentID()

			ui.Info("Starting rollback for application: %s using server %s", appConfig.Name, targetServer)
			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			api := apiclient.New(targetServer, token)
			path := fmt.Sprintf("rollback/%s/%s", appConfig.Name, targetDeploymentID)
			request := apitypes.RollbackRequest{NewDeploymentID: newDeploymentID}
			if err := api.Post(ctx, path, request, nil); err != nil {
				ui.Error("Rollback failed: %v", err)
				return
			}

			if !noLogs {
				// No timeout for streaming logs
				streamCtx, streamCancel := context.WithCancel(context.Background())
				defer streamCancel()
				streamPath := fmt.Sprintf("deploy/%s/logs", newDeploymentID)

				streamHandler := func(data string) bool {
					var logEntry logging.LogEntry
					if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
						ui.Error("failed to ummarshal json: %v", err)
						return false // we don't stop on errors.
					}

					ui.DisplayLogEntry(logEntry, "")

					// If deployment is complete we'll return true to signal stream should stop
					return logEntry.IsDeploymentComplete
				}

				api.Stream(streamCtx, streamPath, streamHandler)
			}
		},
	}

	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream deployment logs")

	return cmd
}

func RollbackTargetsCmd(configPath *string) *cobra.Command {
	var serverURL string

	cmd := &cobra.Command{
		Use:   "rollback-targets",
		Short: "List available rollback targets for an application",
		Long:  `List available rollback targets for an application using a haloy configuration file.`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			appConfig, _, err := config.LoadAppConfig(*configPath)
			if err != nil {
				ui.Error("Failed to load config: %v", err)
				return
			}

			targetServer, err := getServer(appConfig, serverURL)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			token, err := getToken(appConfig, targetServer)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			ui.Info("Rollback targets for application: %s using server %s", appConfig.Name, targetServer)
			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			api := apiclient.New(targetServer, token)
			targets, err := api.RollbackTargets(ctx, appConfig.Name)
			if err != nil {
				ui.Error("Failed to get rollback targets: %v", err)
				return
			}

			if len(targets.Targets) == 0 {
				ui.Info("No rollback targets available for app '%s'", appConfig.Name)
				return
			}

			displayRollbackTargets(appConfig.Name, targets.Targets, *configPath)
		},
	}

	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	return cmd
}

func displayRollbackTargets(appName string, targets []deploytypes.RollbackTarget, configPath string) {
	if len(targets) == 0 {
		ui.Info("No rollback targets available for app '%s'", appName)
		return
	}
	ui.Info("📋 Available rollback targets for '%s':", appName)
	ui.Info("")

	headers := []string{"DEPLOYMENT ID", "IMAGE REFERENCE", "DATE", "STATUS"}
	rows := make([][]string, 0, len(targets))

	for _, target := range targets {

		date := "N/A"
		if deploymentTime, err := helpers.GetTimestampFromDeploymentID(target.DeploymentID); err == nil {
			date = helpers.FormatTime(deploymentTime)
		}

		status := ""
		if target.IsRunning {
			status = "🟢 CURRENT"
		}

		rows = append(rows, []string{
			target.DeploymentID,
			target.ImageRef,
			date,
			status,
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
