package haloy

import (
	"context"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func LogsCmd() *cobra.Command {
	var serverURL string

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream logs from the haloy manager",
		Long: `Stream all logs from the haloy manager in real-time.

This includes:
- Deployment logs
- Certificate renewal logs
- Docker event logs
- HAProxy configuration updates
- General manager activity

The logs are streamed in real-time and will continue until interrupted (Ctrl+C).`,
		Run: func(cmd *cobra.Command, args []string) {
			// Load server URL from config if not provided
			targetServer := serverURL
			if targetServer == "" {
				appConfig, _, err := config.LoadAppConfig(".")
				if err == nil && appConfig.Server != "" {
					targetServer = appConfig.Server
				} else {
					ui.Error("Server URL is required. Use --server flag or configure in haloy config file")
					return
				}
			}

			ui.Info("Connecting to haloy manager at %s", targetServer)
			ui.Info("Streaming all logs... (Press Ctrl+C to stop)")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			api := apiclient.New(targetServer)
			if err := api.StreamLogs(ctx); err != nil {
				ui.Error("Failed to stream logs: %v", err)
				return
			}
		},
	}

	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL")
	return cmd
}
