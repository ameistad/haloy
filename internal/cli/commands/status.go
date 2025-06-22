package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// showStatusTimeout defines the timeout for the status command it doesn't hang.
const showStatusTimeout = 10 * time.Second

func StatusAppCmd() *cobra.Command {
	statusAppCmd := &cobra.Command{
		Use:   "status [app-name]",
		Short: "Show status for all apps or detailed status for a specific app",
		Long: `Show status for all applications if no app name is provided.
If an app name is given, show detailed status including DNS configuration.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ctx, cancel := context.WithTimeout(context.Background(), showStatusTimeout)
			defer cancel()
			cli, err := docker.NewClient(ctx)
			if err != nil {
				ui.Error("Error: %s", err)
				return
			}
			defer cli.Close()

			if len(args) == 0 {
				// Show status for all apps
				containerList, err := docker.GetAppContainers(ctx, cli, true, "")
				if err != nil {
					ui.Error("Error: %s", err)
					return
				}
				if len(containerList) == 0 {
					ui.Info("No running containers found for any app.")
					return
				}
				apps := make(map[string][]container.Summary)
				for _, c := range containerList {
					appName := c.Labels[config.LabelAppName]
					if appName == "" {
						ui.Error("Container %s does not have the required label '%s'. Skipping.", helpers.SafeIDPrefix(c.ID), config.LabelAppName)
						continue
					}
					apps[appName] = append(apps[appName], c)
				}

				for appName, containers := range apps {
					showAppStatus(ctx, cli, appName, containers)
					fmt.Println()
				}
				return
			}

			appName := args[0]
			containerList, err := docker.GetAppContainers(ctx, cli, true, appName)
			if err != nil {
				ui.Error("Error: %s", err)
				return
			}
			if len(containerList) == 0 {
				ui.Info("No running containers found for any app.")
				return
			}
			showAppStatusDetailed(ctx, cli, appName, containerList)

		},
	}
	return statusAppCmd
}

type initialStatus struct {
	state               string
	runningContainerIDs []string
	domains             []config.Domain
	formattedDomains    []string
	envVars             []config.EnvVar
	formattedEnvVars    []string

	// all formatted fields for better output
	formattedOutput []string
}

func getInitialStatus(ctx context.Context, cli *client.Client, appName string, containers []container.Summary) (initialStatus, error) {
	status := initialStatus{}
	state := lipgloss.NewStyle().Foreground(ui.LightGray).Italic(true).Render("Unknown")
	var latestDeploymentID string
	var runningDeploymentID string
	var latestLabels *config.ContainerLabels
	runningContainerIDs := make([]string, 0, len(containers))

	// Find the latest deployment ID and labels, and collect running container IDs.
	for _, c := range containers {
		labels, err := config.ParseContainerLabels(c.Labels)
		if err != nil {
			return status, fmt.Errorf("failed to parse labels for container %s: %w", helpers.SafeIDPrefix(c.ID), err)
		}

		if labels.DeploymentID > latestDeploymentID {
			latestDeploymentID = labels.DeploymentID
			latestLabels = labels
		}

		switch c.State {
		case "running", "restarting":
			if labels.DeploymentID > runningDeploymentID {
				runningDeploymentID = labels.DeploymentID
			}
			runningContainerIDs = append(runningContainerIDs, helpers.SafeIDPrefix(c.ID))
			if c.State == "running" {
				state = lipgloss.NewStyle().Foreground(ui.Green).Render("Running")
			} else if state != "Running" {
				state = lipgloss.NewStyle().Foreground(ui.Amber).Render("Restarting")
			}
		case "exited", "dead":
			state = lipgloss.NewStyle().Foreground(ui.Red).Render("Stopped")

		case "paused":
			state = lipgloss.NewStyle().Foreground(ui.Blue).Render("Paused")

		}
	}
	if latestLabels == nil || latestDeploymentID == "" {
		return status, fmt.Errorf("no valid containers found for app %s", appName)
	}

	formattedDomains := make([]string, 0, len(latestLabels.Domains))
	for _, d := range latestLabels.Domains {
		if len(d.Aliases) == 0 {
			formattedDomains = append(formattedDomains, fmt.Sprintf("  %s", d.Canonical))
		} else {
			aliases := strings.Join(d.Aliases, ", ")
			aliasStyle := lipgloss.NewStyle().Foreground(ui.LightGray).Italic(true)
			styledAliases := aliasStyle.Render(fmt.Sprintf("<- %s", aliases))
			formattedDomains = append(formattedDomains, fmt.Sprintf("  %s %s", d.Canonical, styledAliases))
		}
	}

	// Use latest recorded appCOnfig for deployment ID
	var envVars []config.EnvVar
	var formattedEnvVars []string
	latestAppConfig, _ := deploy.GetAppConfigHistory(latestDeploymentID)
	if latestAppConfig != nil {
		envVars = latestAppConfig.Env
		for _, ev := range latestAppConfig.Env {
			var val string
			if ev.Value != nil {
				val = *ev.Value
			} else if ev.SecretName != nil {
				// If there's a secret reference, simulate the "SECRET:" prefix.
				val = "SECRET:" + *ev.SecretName
			}
			if _, secretName, ok := strings.Cut(val, "SECRET:"); ok {
				formattedEnvVars = append(formattedEnvVars, fmt.Sprintf("  %s: loaded from secret (%s)", ev.Name, secretName))
			} else {
				formattedEnvVars = append(formattedEnvVars, fmt.Sprintf("  %s: %s", ev.Name, val))
			}
		}
	}

	var rollbackMsg string
	rollbackTargets, _ := deploy.GetRollbackTargets(ctx, cli, appName)

	if len(rollbackTargets) == 1 {
		rollbackMsg = "1 rollback image available"
	} else if len(rollbackTargets) > 1 {
		rollbackMsg = fmt.Sprintf("%d rollback images available", len(rollbackTargets))
	} else {
		rollbackMsg = "No rollback images available"
	}

	formattedOutput := []string{
		fmt.Sprintf("State: %s", state),
		fmt.Sprintf("Domains:\n%s", strings.Join(formattedDomains, "\n")),
		fmt.Sprintf("Running container(s): %s", strings.Join(runningContainerIDs, ", ")),
		rollbackMsg,
	}

	if len(formattedEnvVars) > 0 {
		formattedOutput = append(formattedOutput, fmt.Sprintf("Environment Variables:\n%s", strings.Join(formattedEnvVars, "\n")))
	}

	status = initialStatus{
		state:               state,
		runningContainerIDs: runningContainerIDs,
		domains:             latestLabels.Domains,
		formattedDomains:    formattedDomains,
		envVars:             envVars,
		formattedEnvVars:    formattedEnvVars,
		formattedOutput:     formattedOutput,
	}

	return status, nil
}

func showAppStatusDetailed(ctx context.Context, cli *client.Client, appName string, containers []container.Summary) error {

	status, err := getInitialStatus(ctx, cli, appName, containers)
	if err != nil {
		return err
	}

	ui.Section(appName, status.formattedOutput)

	return nil
}

func showAppStatus(ctx context.Context, cli *client.Client, appName string, containers []container.Summary) error {
	status, err := getInitialStatus(ctx, cli, appName, containers)
	if err != nil {
		return err
	}

	ui.Section(appName, status.formattedOutput)

	return nil
}
