package haloy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/appconfigloader"
	"github.com/ameistad/haloy/internal/deploytypes"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func RollbackAppCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var noLogsFlag bool

	cmd := &cobra.Command{
		Use:   "rollback <deployment-id>",
		Short: "Rollback an application to a specific deployment",
		Long: `Rollback an application to a specific deployment by supplying a deployment ID.

Use 'haloy rollback-targets' to list available deployment IDs.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			targetDeploymentID := args[0]

			targets, _, err := appconfigloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			newDeploymentID := createDeploymentID()

			var wg sync.WaitGroup

			for _, target := range targets {
				wg.Add(1)
				go func(target appconfigloader.AppConfigTarget) {
					defer wg.Done()

					appName := target.ResolvedAppConfig.Name
					server := target.ResolvedAppConfig.Server

					token, err := getToken(&target.ResolvedAppConfig, server)
					if err != nil {
						ui.Error("%v", err)
						return
					}
					ui.Info("Starting rollback for application: %s using server %s", appName, server)

					api, err := apiclient.New(server, token)
					if err != nil {
						ui.Error("Failed to create API client: %v", err)
						return
					}
					rollbackTargetsResponse, err := getRollbackTargets(ctx, api, appName)
					if err != nil {
						ui.Error("Failed to get available rollback targets for %s: %v", target.ResolvedAppConfig.TargetName, err)
						return
					}
					var availableTarget deploytypes.RollbackTarget
					for _, at := range rollbackTargetsResponse.Targets {
						if at.DeploymentID == targetDeploymentID {
							availableTarget = at
						}
					}
					if availableTarget.DeploymentID == "" {
						ui.Error("Deployment ID %s not available not found available rollback targets", targetDeploymentID)
					}

					newResolvedAppConfig, err := appconfigloader.ResolveSecrets(ctx, *availableTarget.RawAppConfig)
					if err != nil {
						ui.Error("Unable to resolve secrets for the app config. This usually occurs when secrets names have been changed or deleted between deployments: %v", err)
						return
					}
					request := apitypes.RollbackRequest{
						TargetDeploymentID:   targetDeploymentID,
						NewDeploymentID:      newDeploymentID,
						NewResolvedAppConfig: newResolvedAppConfig,
					}
					if err := api.Post(ctx, "rollback", request, nil); err != nil {
						ui.Error("Rollback failed: %v", err)
						return
					}

					if !noLogsFlag {
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

						api.Stream(ctx, streamPath, streamHandler)
					}
				}(target)
			}

			wg.Wait()
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().BoolVar(&noLogsFlag, "no-logs", false, "Don't stream deployment logs")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Deploy to specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Deploy to all targets")

	return cmd
}

func RollbackTargetsCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback-targets",
		Short: "List available rollback targets for an application",
		Long:  `List available rollback targets for an application using a haloy configuration file.`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			targets, _, err := appconfigloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			var wg sync.WaitGroup

			for _, target := range targets {
				wg.Add(1)

				go func(target appconfigloader.AppConfigTarget) {
					defer wg.Done()

					appName := target.ResolvedAppConfig.Name
					server := target.ResolvedAppConfig.Server

					token, err := getToken(&target.ResolvedAppConfig, server)
					if err != nil {
						ui.Error("%v", err)
						return
					}

					api, err := apiclient.New(server, token)
					if err != nil {
						ui.Error("Failed to create API client: %v", err)
						return
					}
					rollbackTargets, err := getRollbackTargets(ctx, api, appName)
					if err != nil {
						ui.Error("Failed to get rollback targets: %v", err)
						return
					}
					if len(rollbackTargets.Targets) == 0 {
						ui.Info("No rollback targets available for app '%s'", appName)
						return
					}

					displayRollbackTargets(appName, rollbackTargets.Targets, *configPath, target.ResolvedAppConfig.TargetName)
				}(target)
			}

			wg.Wait()
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Deploy to specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Deploy to all targets")

	return cmd
}

func getRollbackTargets(ctx context.Context, api *apiclient.APIClient, appName string) (*apitypes.RollbackTargetsResponse, error) {
	path := fmt.Sprintf("rollback/%s", appName)
	var response apitypes.RollbackTargetsResponse
	if err := api.Get(ctx, path, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func displayRollbackTargets(appName string, rollbackTargets []deploytypes.RollbackTarget, configPath, targetName string) {
	if len(rollbackTargets) == 0 {
		ui.Info("No rollback targets available for app '%s'", appName)
		return
	}

	header := fmt.Sprintf("Available rollback targets for '%s':", appName)
	if targetName != "" {
		header = fmt.Sprintf("%s on %s", header, targetName)
	}
	ui.Info("%s", header)

	headers := []string{"DEPLOYMENT ID", "IMAGE REFERENCE", "DATE", "STATUS"}
	rows := make([][]string, 0, len(rollbackTargets))

	for _, rollbackTarget := range rollbackTargets {

		date := "N/A"
		if deploymentTime, err := helpers.GetTimestampFromDeploymentID(rollbackTarget.DeploymentID); err == nil {
			date = helpers.FormatTime(deploymentTime)
		}

		status := ""
		if rollbackTarget.IsRunning {
			status = "🟢 CURRENT"
		}

		rows = append(rows, []string{
			rollbackTarget.DeploymentID,
			rollbackTarget.ImageRef,
			date,
			status,
		})
	}

	ui.Table(headers, rows)
	ui.Basic("To rollback, run:")
	ui.Basic("  haloy rollback <deployment-id>")
	if configPath != "." {
		ui.Basic("  # Or with explicit config:")
		ui.Basic("  haloy rollback --config %s <deployment-id>", configPath)
	}
}
