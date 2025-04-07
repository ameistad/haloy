package commands

import (
	"context"
	"time"

	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	startTimeout = 5 * time.Minute
)

func StartCmd() *cobra.Command {

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the haloy services",
		Long:  "Start the haloy services, including HAProxy and haloy-manager.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
			defer cancel()
			dockerClient, err := docker.NewClient(ctx)
			if err != nil {
				ui.Error("%v", err)
				return
			}
			defer dockerClient.Close()

			// Start the haloy services
			status, err := docker.EnsureServicesIsRunning(dockerClient, ctx)
			if err != nil {
				ui.Error("Failed to start services: %v", err)
				return
			}

			if status.State == docker.ServiceStateRunning {
				ui.Success("Haloy services are already running")
			} else {
				ui.Success("Haloy services started successfully")
			}
			return

		},
	}
	return cmd
}
