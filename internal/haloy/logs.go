package haloy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/appconfigloader"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func LogsCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag string

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream logs from haloy server",
		Long: `Stream all logs from haloy server in real-time.

The logs are streamed in real-time and will continue until interrupted (Ctrl+C).`,
		Args: cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			ctx := cmd.Context()
			if serverFlag != "" {
				streamLogs(ctx, nil, serverFlag)
			} else {
				rawAppConfig, err := appconfigloader.LoadImproved(ctx, *configPath, flags.targets, flags.all)
				if err != nil {
					ui.Error("%v", err)
					return
				}

				targets, err := appconfigloader.ResolveTargets(rawAppConfig)
				if err != nil {
					ui.Error("Unable to create deploy targets: %v", err)
					return
				}

				var wg sync.WaitGroup
				for _, target := range targets {
					wg.Add(1)
					go func(appConfig config.AppConfig) {
						defer wg.Done()
						streamLogs(ctx, &appConfig, target.Server)
					}(target)
				}

				wg.Wait()
			}
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Haloy server URL")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Show logs for specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Show all target logs")

	return cmd
}

func streamLogs(ctx context.Context, appConfig *config.AppConfig, targetServer string) error {
	token, err := getToken(appConfig, targetServer)
	if err != nil {
		return err
	}

	ui.Info("Connecting to haloy server at %s", targetServer)
	ui.Info("Streaming all logs... (Press Ctrl+C to stop)")

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		return fmt.Errorf("Failed to create API client: %w", err)
	}
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
	return api.Stream(ctx, "logs", streamHandler)
}
