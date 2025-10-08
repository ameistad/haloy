package haloyadm

import (
	"os"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func RestartCmd() *cobra.Command {
	var devMode bool
	var debug bool
	var noLogs bool

	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the haloy services",
		Long:  "Restart the haloy services, including HAProxy and haloyd.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()

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

			if err := ensureNetwork(ctx); err != nil {
				ui.Error("Failed to ensure Docker network exists: %v", err)
				ui.Info("You can manually create it with:")
				ui.Info("docker network create --driver bridge --attachable %s", constants.DockerNetwork)
				return
			}

			if err := startServices(ctx, dataDir, configDir, devMode, true, debug); err != nil {
				ui.Error("%s", err)
				return
			}

			if !noLogs {
				ui.Info("Waiting for haloyd API to become available...")
				token := os.Getenv(constants.EnvVarAPIToken)
				if token == "" {
					ui.Error("Failed to get API token")
					return
				}

				// Wait for API to become available and stream init logs
				if err := streamHaloydInitLogs(ctx, token); err != nil {
					ui.Warn("Failed to stream haloyd initialization logs: %v", err)
					ui.Info("haloyd is starting in the background. Check logs with: docker logs haloyd")
				}
			}
		},
	}
	cmd.Flags().BoolVar(&devMode, "dev", false, "Start in development mode using the local haloyd image")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug mode")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream haloyd initialization logs")

	return cmd
}
