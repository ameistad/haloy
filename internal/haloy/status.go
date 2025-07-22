package haloy

import (
	"context"
	"fmt"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func StatusAppCmd() *cobra.Command {
	var configPath string
	var serverURL string
	cmd := &cobra.Command{
		Use:   "status [config-path]",
		Short: "Show status for an application",
		Long: `Show current status of a deployed application using a haloy configuration file.

The path can be:
- A directory containing haloy.json, haloy.yaml, haloy.yml, or haloy.toml
- A full path to a config file with supported extension
- A relative path to either of the above

If no path is provided, the current directory is used.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) > 0 {
				configPath = args[0]
			} else if configPath == "" {
				configPath = "."
			}

			appConfig, err := config.LoadAndValidateAppConfig(configPath)
			if err != nil {
				ui.Error("Failed to load config: %v", err)
				return
			}
			targetServer := appConfig.Server
			if serverURL != "" {
				targetServer = serverURL
			}

			ui.Info("Getting status for application: %s using server %s", appConfig.Name, targetServer)
			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			apiClient := NewAPIClient(targetServer)
			status, err := apiClient.AppStatus(ctx, appConfig.Name)
			if err != nil {
				ui.Error("Failed to get app status: %v", err)
				return
			}

			containerIDs := make([]string, len(status.ContainerIDs))
			for _, id := range status.ContainerIDs {
				containerIDs = append(containerIDs, helpers.SafeIDPrefix(id))
			}

			state := displayState(status.State)
			formattedOutput := []string{
				fmt.Sprintf("State: %s", state),
				fmt.Sprintf("Deployment ID: %s", status.DeploymentID),
				fmt.Sprintf("Running container(s): %s", strings.Join(containerIDs, ", ")),
			}

			ui.Section(fmt.Sprintf("Status for %s", appConfig.Name), formattedOutput)
		},
	}
	return cmd
}

func displayState(state string) string {
	switch strings.ToLower(state) {
	case "running":
		return lipgloss.NewStyle().Foreground(ui.Green).Render("Running")
	case "restarting":
		return lipgloss.NewStyle().Foreground(ui.Amber).Render("Restarting")
	case "paused":
		return lipgloss.NewStyle().Foreground(ui.Blue).Render("Paused")
	case "exited":
		return lipgloss.NewStyle().Foreground(ui.Red).Render("Exited")
	case "stopped":
		return lipgloss.NewStyle().Foreground(ui.Red).Render("Stopped")
	default:
		return lipgloss.NewStyle().Foreground(ui.LightGray).Italic(true).Render(strings.Title(state))
	}
}
