package commands

import (
	"context" // Import context
	"errors"  // Import errors
	"fmt"     // Import fmt
	"os"      // Import os
	"os/signal"
	"syscall"
	"time" // Import time

	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui" // Import ui
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

func LogsCmd() *cobra.Command {
	var appFilter string
	var showDebug bool

	cmd := &cobra.Command{
		Use:   "logs [-a appName] [--debug]",
		Short: "Stream logs from the haloy manager",
		Long:  `Connects to the haloy manager's log stream and displays logs, optionally filtering by application name and including debug messages.`,
		Run: func(cmd *cobra.Command, args []string) {
			minLevel := zerolog.InfoLevel // Default to Info
			if showDebug {
				minLevel = zerolog.DebugLevel // Set to Debug if flag is true
			}

			clientConfig := logging.ClientConfig{
				AppNameFilter: appFilter,
				UseDeadline:   false,            // Logs command usually streams continuously
				MinLevel:      minLevel,         // Set the determined level
				DialTimeout:   10 * time.Second, // Add a reasonable dial timeout
			}

			ui.Info("Attempting to connect to log stream at %s...\n", logging.DefaultStreamAddress)
			client, err := logging.NewLogStreamClient(clientConfig)
			if err != nil {
				ui.Error("Failed to connect to log stream: %v\n", err)
				return
			}
			defer client.Close()

			filterMsg := "all logs"
			if appFilter != "" {
				filterMsg = fmt.Sprintf("logs for '%s'", appFilter)
			}
			levelMsg := "info level and above"
			if showDebug {
				levelMsg = "debug level and above"
			}
			ui.Success("Connected to log stream. Displaying %s (%s). Press Ctrl+C to stop.\n", filterMsg, levelMsg)

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			streamErrChan := make(chan error, 1)
			go func() {
				streamErrChan <- client.Stream(ctx, os.Stdout)
			}()

			select {
			case <-ctx.Done():
				ui.Info("\nDisconnecting from logs...\n")
				// Wait for stream to finish after context cancellation
				<-streamErrChan
			case err := <-streamErrChan:
				// Handle errors from the stream itself
				if err != nil && !errors.Is(err, context.Canceled) {
					ui.Error("Log stream error: %v\n", err)
				} else if err == nil {
					// Stream ended cleanly (e.g., server closed connection)
					ui.Info("Log stream connection closed by server.\n")
				}
				// If context was cancelled, the ctx.Done case handles the message
			}
			// --- End of connection/streaming logic ---
		},
	}

	cmd.Flags().StringVarP(&appFilter, "app", "a", "", "Filter logs by app name")
	cmd.Flags().BoolVar(&showDebug, "debug", false, "Include debug level messages in the stream")

	return cmd
}
