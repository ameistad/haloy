package haloy

import (
	"context"
	"fmt"
	"strings"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func StatusAppCmd(configPath *string) *cobra.Command {
	var serverURL string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show status for an application",
		Long:  "Show current status of a deployed application using a haloy configuration file.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			appConfig, _, err := config.LoadAppConfig(*configPath)
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

			ui.Info("Getting status for application: %s using server %s", appConfig.Name, targetServer)
			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			api := apiclient.New(targetServer, token)
			status, err := api.AppStatus(ctx, appConfig.Name)
			if err != nil {
				ui.Error("Failed to get app status: %v", err)
				return
			}

			containerIDs := make([]string, 0, len(status.ContainerIDs))
			for _, id := range status.ContainerIDs {
				containerIDs = append(containerIDs, helpers.SafeIDPrefix(id))
			}

			canonicalDomains := make([]string, 0, len(status.Domains))
			for _, domain := range status.Domains {
				canonicalDomains = append(canonicalDomains, domain.Canonical)
			}

			state := displayState(status.State)
			formattedOutput := []string{
				fmt.Sprintf("State: %s", state),
				fmt.Sprintf("Deployment ID: %s", status.DeploymentID),
				fmt.Sprintf("Running container(s): %s", strings.Join(containerIDs, ", ")),
				fmt.Sprintf("Domain(s): %s", strings.Join(canonicalDomains, ", ")),
			}

			ui.Section(fmt.Sprintf("Status for %s", appConfig.Name), formattedOutput)
		},
	}
	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
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
		return lipgloss.NewStyle().Foreground(ui.LightGray).Italic(true).Render(state)
	}
}
