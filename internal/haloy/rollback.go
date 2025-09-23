package haloy

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sync"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/config"
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
			targetDeploymentID := args[0]

			appConfig, _, err := config.LoadAppConfig(*configPath)
			if err != nil {
				ui.Error("Failed to load config: %v", err)
				return
			}

			targets, err := expandTargets(appConfig, flags.targets, flags.all)
			if err != nil {
				ui.Error("Failed to process deployment targets: %v", err)
				return
			}

			newDeploymentID := createDeploymentID()

			var wg sync.WaitGroup

			for _, target := range targets {
				wg.Add(1)
				go func(target ExpandedTarget) {
					defer wg.Done()

					targetServer := target.Config.Server

					token, err := getToken(&appConfig, targetServer)
					if err != nil {
						ui.Error("%v", err)
						return
					}
					ui.Info("Starting rollback for application: %s using server %s", appConfig.Name, targetServer)
					ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
					defer cancel()

					api, err := apiclient.New(targetServer, token)
					if err != nil {
						ui.Error("Failed to create API client: %v", err)
						return
					}
					rollbackTargetsResponse, err := getRollbackTargets(ctx, api, target.Config.Name)
					if err != nil {
						ui.Error("Failed to get available rollback targets for %s: %v", target.TargetName, err)
						return
					}
					availableTargets := rollbackTargetsResponse.Targets
					availableTargetDeploymentIDs := make([]string, 0, len(availableTargets))
					for _, availableTarget := range availableTargets {
						availableTargetDeploymentIDs = append(availableTargetDeploymentIDs, availableTarget.DeploymentID)
					}
					if !slices.Contains(availableTargetDeploymentIDs, targetDeploymentID) {
						ui.Error("Target deployment id: %s is not available in %s", targetDeploymentID, target.TargetName)
						return
					}
					path := fmt.Sprintf("rollback/%s", appConfig.Name)
					request := apitypes.RollbackRequest{TargetDeploymentID: targetDeploymentID, NewDeploymentID: newDeploymentID}
					if err := api.Post(ctx, path, request, nil); err != nil {
						ui.Error("Rollback failed: %v", err)
						return
					}

					if !noLogsFlag {
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
				}(target)

				wg.Wait()
			}
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
			appConfig, _, err := config.LoadAppConfig(*configPath)
			if err != nil {
				ui.Error("Failed to load config: %v", err)
				return
			}

			targets, err := expandTargets(appConfig, flags.targets, flags.all)
			if err != nil {
				ui.Error("Failed to process deployment targets: %v", err)
				return
			}

			var wg sync.WaitGroup

			for _, target := range targets {
				wg.Add(1)

				go func(target ExpandedTarget) {
					defer wg.Done()

					targetServer := target.Config.Server

					token, err := getToken(&target.Config, targetServer)
					if err != nil {
						ui.Error("%v", err)
						return
					}

					ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
					defer cancel()

					api, err := apiclient.New(targetServer, token)
					if err != nil {
						ui.Error("Failed to create API client: %v", err)
						return
					}
					rollbackTargets, err := getRollbackTargets(ctx, api, appConfig.Name)
					if err != nil {
						ui.Error("Failed to get rollback targets: %v", err)
						return
					}
					if len(rollbackTargets.Targets) == 0 {
						ui.Info("No rollback targets available for app '%s'", appConfig.Name)
						return
					}

					displayRollbackTargets(appConfig.Name, rollbackTargets.Targets, *configPath, target.TargetName)
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
	if configPath == "." {
		ui.Basic("  haloy rollback <deployment-id>")
	} else {
		ui.Basic("  haloy rollback %s <deployment-id>", configPath)
	}
}
