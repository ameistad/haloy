package commands

import (
	"fmt"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploy"
	"github.com/spf13/cobra"
)

func RollbackAppCmd() *cobra.Command {
	rollbackAppCmd := &cobra.Command{
		Use:   "rollback <app-name>",
		Short: "Rollback an application",
		Long:  `Rollback an application to a previous container image`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appName := args[0]
			appConfig, err := config.AppConfigByName(appName)
			if err != nil {
				return err
			}

			// Retrieve container flag if provided.
			containerIDFlag, _ := cmd.Flags().GetString("container")
			var targetContainerID string

			sortedContainers, err := deploy.SortedContainerInfo(appConfig)
			if err != nil {
				return err
			}

			if len(sortedContainers) < 2 {
				return fmt.Errorf("you only have one container for app %s, cannot rollback", appConfig.Name)
			}
			currentContainerID := sortedContainers[0].ID

			if containerIDFlag != "" {
				// Check if containerIDFlag is in sortedContainers and is not sortedContainers[0].
				if sortedContainers[0].ID == containerIDFlag {
					return fmt.Errorf("container %s is already the current container", containerIDFlag)
				}

				// if conatinerIDFlag is not in sortedContainers, return an error.
				found := false
				for _, container := range sortedContainers {
					if container.ID == containerIDFlag {
						targetContainerID = container.ID
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("container %s is not part of the deployment, check running containers with docker ps -a", containerIDFlag)
				}
			} else {
				targetContainerID = sortedContainers[1].ID
			}

			fmt.Printf("Current container: %s\n", currentContainerID)
			fmt.Printf("Rolling back app '%s' to container %s\n", appConfig.Name, targetContainerID)
			if err := deploy.RollbackToContainer(currentContainerID, targetContainerID, appConfig.HealthCheckPath); err != nil {
				return fmt.Errorf("rollback failed: %w", err)
			}

			return nil
		},
	}

	rollbackAppCmd.Flags().StringP("container", "c", "", "Specify container ID to use for rollback")
	return rollbackAppCmd
}
