package haloy

import (
	"context"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func LogsCmd() *cobra.Command {
	var configPath string
	var serverURL string

	cmd := &cobra.Command{
		Use:   "logs [config-path]",
		Short: "Stream logs from the haloy manager",
		Long: `Stream all logs from the haloy manager in real-time.

		The path can be:
- A directory containing haloy.json, haloy.yaml, haloy.yml, or haloy.toml
- A full path to a config file with supported extension
- A relative path to either of the above

If no path is provided, the current directory is used.

The logs are streamed in real-time and will continue until interrupted (Ctrl+C).`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) > 0 {
				configPath = args[0]
			} else if configPath == "" {
				configPath = "."
			}

			appConfig, _, err := config.LoadAppConfig(configPath)
			if err != nil {
				ui.Error("Failed to load config: %v", err)
				return
			}

			targetServer, err := getServer(appConfig, serverURL)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			token, err := getToken(targetServer)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			ui.Info("Connecting to haloy manager at %s", targetServer)
			ui.Info("Streaming all logs... (Press Ctrl+C to stop)")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			api := apiclient.New(targetServer, token)
			if err := api.StreamLogs(ctx); err != nil {
				ui.Error("Failed to stream logs: %v", err)
				return
			}
		},
	}

	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL")
	return cmd
}
