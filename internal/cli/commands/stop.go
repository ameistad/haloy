package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	stopAppTimeout = 2 * time.Minute // Timeout for stop operations
)

func StopAppCmd() *cobra.Command {
	var removeContainersFlag bool

	cmd := &cobra.Command{
		Use:   "stop <app-name>",
		Short: "Stop an application's running containers",
		Long: `Stops all running containers associated with the specified application.
Containers are identified by the 'haloy.app=<app-name>' label.
Optionally, it can also remove the containers after stopping them.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("stop command requires exactly one argument: the app name (e.g., 'haloy stop my-app')")
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			appName := args[0]

			ctx, cancel := context.WithTimeout(context.Background(), stopAppTimeout)
			defer cancel()

			dockerClient, err := docker.NewClient(ctx)
			if err != nil {
				ui.Error("Failed to create Docker client: %v\n", err)
				return
			}
			defer dockerClient.Close()

			stoppedIDs, err := docker.StopContainers(ctx, dockerClient, appName, "")
			if err != nil {
				ui.Error("Error while stopping containers for app %q: %v\n", appName, err)
				// If stopping failed and nothing was stopped, it's probably best to return.
				if len(stoppedIDs) == 0 {
					return
				}
			}

			if len(stoppedIDs) > 0 {
				ui.Success("Successfully stopped %d container(s) for app %q.\n", len(stoppedIDs), appName)
			} else {
				ui.Info("No running containers found for app %q to stop.\n", appName)
			}

			if removeContainersFlag {
				ui.Info("Attempting to remove containers for app %q...\n", appName)
				removedIDs, removeErr := docker.RemoveContainers(ctx, dockerClient, appName, "")
				if removeErr != nil {
					ui.Error("Error while removing containers for app %q: %v\n", appName, removeErr)
				}

				if len(removedIDs) > 0 {
					ui.Success("Successfully removed %d container(s) for app %q.\n", len(removedIDs), appName)
				} else {
					if removeErr == nil { // No error, but no containers removed
						ui.Info("No containers found for app %q to remove.\n", appName)
					}
					// If removeErr != nil, the error message was already printed.
				}
			}
		},
	}

	cmd.Flags().BoolVarP(&removeContainersFlag, "remove-containers", "r", false, "Remove the containers after stopping them")
	return cmd
}
