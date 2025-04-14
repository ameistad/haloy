package commands

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func LogsCmd() *cobra.Command {
	var appFilter string

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View logs from the haloy manager",
		Run: func(cmd *cobra.Command, args []string) {
			host := os.Getenv("HALOY_HOST")
			if host == "" {
				host = "localhost"
			}

			// Connect to the log server
			conn, err := net.Dial("tcp", fmt.Sprintf("%s:9000", host))
			if err != nil {
				ui.Error("Failed to connect to log server: %v\n", err)
				return
			}
			defer conn.Close()

			// Send filter
			fmt.Fprintf(conn, "%s\n", appFilter)

			// Set up signal handling for clean exit
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

			// Start reading logs in a goroutine
			done := make(chan struct{})
			go func() {
				defer close(done)
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					line := scanner.Text()
					// Format the output nicely with indentation and line wrapping if needed
					fmt.Println(line)
				}
			}()

			// Wait for either interrupt signal or EOF
			select {
			case <-sigCh:
				ui.Info("\nDisconnecting from logs...\n")
			case <-done:
				if appFilter == "" {
					ui.Error("Log connection closed by server\n")
				} else {
					ui.Error("Log connection closed by server while filtering for '%s'\n", appFilter)
				}
			}
		},
	}

	cmd.Flags().StringVarP(&appFilter, "app", "a", "", "Filter logs by app name")

	return cmd
}
