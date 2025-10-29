package haloyadm

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ameistad/haloy/internal/apiclient"
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

			waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
			defer waitCancel()

			ui.Info("Waiting for HAProxy to become available...")
			if err := waitForHAProxy(waitCtx); err != nil {
				ui.Error("HAProxy failed to become ready: %v", err)
				return
			}

			if !noLogs {
				ui.Info("Waiting for haloyd API to become available...")
				apiToken := os.Getenv(constants.EnvVarAPIToken)
				if apiToken == "" {
					ui.Error("Failed to get API token")
					return
				}
				apiURL := fmt.Sprintf("http://localhost:%s", constants.APIServerPort)
				api, err := apiclient.New(apiURL, apiToken)
				if err != nil {
					ui.Error("Failed to create API client: %v", err)
					return
				}

				ui.Info("Waiting for haloyd API to become available...")
				if err := waitForAPI(waitCtx, api); err != nil {
					ui.Error("Haloyd API not available: %v", err)
					return
				}
				if err := streamHaloydInitLogs(ctx, api); err != nil {
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
