package haloyadm

import (
	"context"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	startTimeout = 5 * time.Minute
)

func StartCmd() *cobra.Command {
	var devMode bool
	var restart bool
	var debug bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the haloy services",
		Long:  "Start the haloy services, including HAProxy and haloy-manager.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {

			ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
			defer cancel()

			dataDir, err := config.DataDir()
			if err != nil {
				ui.Error("Failed to determine data directory: %v\n", err)
				return
			}

			configDir, err := config.ConfigDir()
			if err != nil {
				ui.Error("Failed to determine config directory: %v\n", err)
				return
			}

			// Ensure Docker network exists before starting services
			if err := ensureNetwork(ctx); err != nil {
				ui.Error("Failed to ensure Docker network exists: %v", err)
				ui.Info("You can manually create it with:")
				ui.Info("docker network create --driver bridge --attachable %s", constants.DockerNetwork)
				return
			}

			if err := startServices(ctx, dataDir, configDir, devMode, restart, debug); err != nil {
				ui.Error("%s", err)
				return
			}
			ui.Success("Haloy services started successfully")
		},
	}
	cmd.Flags().BoolVar(&devMode, "dev", false, "Start in development mode using the local haloy-manager image")
	cmd.Flags().BoolVar(&restart, "restart", false, "Restart services if they are already running")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug mode")
	return cmd
}
