package haloyadm

import (
	"context"
	"os"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	startTimeout   = 5 * time.Minute
	apiWaitTimeout = 30 * time.Second
)

func StartCmd() *cobra.Command {
	var devMode bool
	var restart bool
	var debug bool
	var noLogs bool

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

			if !noLogs {
				ui.Info("Waiting for manager API to become available...")
				token := os.Getenv(constants.EnvVarAPIToken)
				if token == "" {
					ui.Error("Failed to get API token")
					return
				}

				// Wait for API to become available and stream init logs
				if err := streamManagerInitLogs(ctx, token); err != nil {
					ui.Warn("Failed to stream manager initialization logs: %v", err)
					ui.Info("Manager is starting in the background. Check logs with: docker logs haloy-manager")
				}
			}
		},
	}
	cmd.Flags().BoolVar(&devMode, "dev", false, "Start in development mode using the local haloy-manager image")
	cmd.Flags().BoolVar(&restart, "restart", false, "Restart services if they are already running")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug mode")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream manager initialization logs")

	return cmd
}
