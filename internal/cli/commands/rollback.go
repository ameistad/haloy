package commands

import (
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func RollbackAppCmd() *cobra.Command {
	rollbackAppCmd := &cobra.Command{
		Use:   "rollback <app-name>",
		Short: "Rollback an application",
		Long:  `Rollback an application to a previous deployment`,
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			appName := args[0]
			appConfig, err := config.AppConfigByName(appName)
			if err != nil {
				ui.Error("Failed to get configuration for %q: %v\n", appName, err)
				return
			}

			// Retrieve container flag if provided.
			deploymentIDFlag, _ := cmd.Flags().GetString("deployment")
			var targetDeploymentID string
			if deploymentIDFlag != "" {
				targetDeploymentID = deploymentIDFlag
			}

			if err := deploy.RollbackApp(appConfig, targetDeploymentID); err != nil {
				ui.Error("Failed to rollback %q: %v\n", appName, err)
			} else {
				ui.Success("Rollback of %s completed successfully.\n", appName)
			}
		},
	}

	rollbackAppCmd.Flags().StringP("deployment", "d", "", "Specify deployment ID to use for rollback")
	return rollbackAppCmd
}
