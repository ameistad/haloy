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
			cli, err := docker.NewClient(ctx)
			if err != nil {
				ui.Error("%v", err)
				return
			}
			defer cli.Close()

			// Start the haloy services
			status, err := docker.EnsureServicesIsRunning(cli, ctx)
			if err != nil {
				ui.Error("Failed to start services: %v", err)
				return
			}

			if status.State == docker.ServiceStateRunning {
				ui.Success("Haloy services are already running\n")
			} else {
				ui.Success("Haloy services started successfully\n")
			}
		},
	}
	return cmd
}
