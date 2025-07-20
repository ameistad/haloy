package haloy

import (
	"context"
	"fmt"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func StatusAppCmd() *cobra.Command {
	var configPath string
	var serverURL string
	ExecuteCommand := &cobra.Command{
		Use:   "status [config-path]",
		Short: "Show status for an app",
		Long:  ``,
		Args:  cobra.MaximumNArgs(1),
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

			ctx, cancel := context.WithTimeout(context.Background(), deploy.DefaultContextTimeout)
			defer cancel()

			apiClient := NewAPIClient(targetServer)
			status, err := apiClient.AppStatus(ctx, appConfig.Name)
			if err != nil {
				ui.Error("Failed to get app status: %v", err)
				return
			}

			state := displayState(status.State)
			formattedOutput := []string{
				fmt.Sprintf("State: %s", state),
				fmt.Sprintf("Deployment ID:\n%s", status.DeploymentID),
				fmt.Sprintf("Running container(s): %s", strings.Join(status.ContainerIDs, ", ")),
			}

			ui.Section(appConfig.Name, formattedOutput)
		},
	}
	return ExecuteCommand
}

func displayState(state string) string {
	switch state {
	case "running":
		return lipgloss.NewStyle().Foreground(ui.Green).Render(state)
	case "stopped":
		return lipgloss.NewStyle().Foreground(ui.Red).Render(state)
	case "paused":
		return lipgloss.NewStyle().Foreground(ui.Blue).Render(state)
	default:
		return lipgloss.NewStyle().Foreground(ui.LightGray).Italic(true).Render(state)
	}
}
