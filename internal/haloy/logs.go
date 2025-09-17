package haloy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func LogsCmd() *cobra.Command {
	var configPath string
	var serverURL string

	cmd := &cobra.Command{
		Use:   "logs [config-path]",
		Short: "Stream logs from haloy server",
		Long: `Stream all logs from haloy server in real-time.

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

			token, err := getToken(appConfig, targetServer)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			ui.Info("Connecting to haloy server at %s", targetServer)
			ui.Info("Streaming all logs... (Press Ctrl+C to stop)")

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			api := apiclient.New(targetServer, token)
			streamHandler := func(data string) bool {
				var logEntry logging.LogEntry
				if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
					ui.Error("failed to parse log entry: %v", err)
				}

				prefix := ""
				if logEntry.DeploymentID != "" {
					prefix = fmt.Sprintf("[id: %s] -> ", logEntry.DeploymentID[:8])
				}

				ui.DisplayLogEntry(logEntry, prefix)

				// Never stop streaming for general logs
				return false
			}
			api.Stream(ctx, "logs", streamHandler)
		},
	}

	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL")
	return cmd
}
