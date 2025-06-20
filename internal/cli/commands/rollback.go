package commands

import (
	"context"

	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

func RollbackAppCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback <app-name> [deployment-id]",
		Short: "Rollback an application",
		Long:  `Rollback an application to a previous deployment. If no deployment id is provided, available rollback targets will be listed.`,
		Args:  cobra.RangeArgs(1, 2),
		Run: func(cmd *cobra.Command, args []string) {
			appName := args[0]

			if appName == "" {
				ui.Error("Please provide an application name to rollback.")
				ui.Info("Usage: haloy rollback <app-name> [deployment-id]")
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), deploy.DefaultDeployTimeout)
			defer cancel()
			cli, err := docker.NewClient(ctx)
			if err != nil {
				ui.Error("Failed to create Docker client: %v", err)
				return
			}
			defer cli.Close()

			var targetDeploymentID string
			if len(args) == 2 {
				targetDeploymentID = args[1]
			}

			if targetDeploymentID == "" {
				listRollbackTargets(ctx, cli, appName)
			} else {
				// Execute rollback with provided deployment id.
				if err := deploy.RollbackApp(ctx, cli, appName, targetDeploymentID); err != nil {
					ui.Error("Failed to rollback %q: %v\n", appName, err)
				} else {
					ui.Success("Rollback of %s completed successfully.\n", appName)
				}
			}
		},
	}

	return cmd
}

// RollbackListCmd retrieves and lists available rollback targets for a given app.
func RollbackListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rollback-list <app-name>",
		Short: "List available rollback targets for an application",
		Long: `List available rollback targets for an application.
This command displays previous deployment targets (sorted by deployment ID, newest first)
so you can choose which one to rollback to.`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			appName := args[0]

			if appName == "" {
				ui.Error("Please provide an application name to view available rollback targets.")
				ui.Info("Usage: haloy rollback-list <app-name>]")
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), deploy.DefaultDeployTimeout)
			defer cancel()
			cli, err := docker.NewClient(ctx)
			if err != nil {
				ui.Error("Failed to create Docker client: %v", err)
				return
			}
			defer cli.Close()

			listRollbackTargets(ctx, cli, appName)
		},
	}

	return cmd
}

func listRollbackTargets(ctx context.Context, cli *client.Client, appName string) {
	targets, err := deploy.GetRollbackTargets(ctx, cli, appName)
	if err != nil {
		ui.Error("failed to retrieve rollback targets for %q: %v", appName, err)
		return
	}

	if len(targets) == 0 {
		ui.Info("there are no images to rollback to for")
		return
	}

	headers := []string{"DEPLOYMENT ID", "IMAGE ID", "DATE"}
	rows := make([][]string, 0, len(targets))
	for _, t := range targets {
		date, err := helpers.FormatDateString(t.DeploymentID)
		if err != nil {
			ui.Error("failed to parse deployment ID %q: %v", t.DeploymentID, err)
			continue
		}
		rows = append(rows, []string{
			t.DeploymentID,
			t.ImageTag,
			date,
		})
	}
	ui.Table(headers, rows)
	ui.Basic("You can specify a deployment ID to rollback to a specific target.")
	ui.Basic("Use 'haloy rollback %s <deployment-id>' to rollback to a specific deployment.", appName)
}
