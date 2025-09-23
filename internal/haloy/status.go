package haloy

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func StatusAppCmd(configPath *string) *cobra.Command {
	var targetFlag string
	var allFlag bool

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

			targets, err := expandTargets(appConfig, targetFlag, allFlag)
			if err != nil {
				ui.Error("Failed to process deployment targets: %v", err)
				return
			}

			var wg sync.WaitGroup
			for _, target := range targets {
				wg.Add(1)
				go func(target ExpandedTarget) {
					defer wg.Done()
					getAppStatus(&appConfig, target.Config.Server, appConfig.Name)
				}(target)
			}

			wg.Wait()
		},
	}

	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "Show status for a specific target")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Show status for all targets")
	return cmd
}

func getAppStatus(appConfig *config.AppConfig, targetServer, appName string) {
	token, err := getToken(appConfig, targetServer)
	if err != nil {
		ui.Error("%v", err)
		return
	}

	ui.Info("Getting status for application: %s using server %s", appName, targetServer)
	ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
	defer cancel()

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		ui.Error("Failed to create API client: %v", err)
		return
	}
	status, err := api.AppStatus(ctx, appName)
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

	ui.Section(fmt.Sprintf("Status for %s", appName), formattedOutput)
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
