package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

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
				configFilePath, err := config.ConfigFilePath()
				if err != nil {
					ui.Error("Error: %s", err)
					return
				}
				configFile, err := config.LoadAndValidateConfig(configFilePath)
				if err != nil {
					ui.Error("Error: %s", err)
					return
				}
				for i := range configFile.Apps {
					if err := showAppStatus(ctx, cli, &configFile.Apps[i]); err != nil {
						ui.Error("Error: %s", err)
					}
				}
				return
			}

			appName := args[0]
			appConfig, err := config.AppConfigByName(appName)
			if err != nil {
				ui.Error("Error: %s", err)
				return
			}

			if err := showAppStatusDetailed(ctx, cli, appConfig); err != nil {
				ui.Error("Error: %s", err)
			}

		},
	}
	return statusAppCmd
}

const (
	showStatusTimeout = 5 * time.Second
)

type initialStatus struct {
	state                         string
	runningContainerIDs           []string
	availableRollbackContainerIDs []string
	domains                       []config.Domain
	formattedDomains              []string
	envVars                       []config.EnvVar
	formattedEnvVars              []string

	// all formatted fields for better output
	formattedOutput []string
}

func getInitialStatus(ctx context.Context, cli *client.Client, appConfig *config.AppConfig) (initialStatus, error) {
	status := initialStatus{}
	filtersArgs := filters.NewArgs()
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelRole, config.AppLabelRole))
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, appConfig.Name))

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filtersArgs,
		All:     true,
	})
	if err != nil {
		return status, fmt.Errorf("failed to get container for app %s: %w", appConfig.Name, err)
	}

	state := "Not running"
	var runningDeploymentID string
	runningContainerIDs := make([]string, 0, len(containers))
	availableRollbackContainerIDs := make([]string, 0, len(containers))

	// First pass: find running containers and determine the highest deploymentID.
	for _, c := range containers {
		labels, err := config.ParseContainerLabels(c.Labels)
		if err != nil {
			return status, fmt.Errorf("failed to parse labels for container %s: %w", helpers.SafeIDPrefix(c.ID), err)
		}

		if c.State == "running" || c.State == "restarting" {
			if labels.DeploymentID > runningDeploymentID {
				runningDeploymentID = labels.DeploymentID
			}
			runningContainerIDs = append(runningContainerIDs, helpers.SafeIDPrefix(c.ID))
			if c.State == "running" {
				state = "Running"
			} else if state != "Running" {
				state = "Restarting"
			}
		}
	}
	// Second pass: count rollback containers.
	for _, c := range containers {
		labels, err := config.ParseContainerLabels(c.Labels)
		if err != nil {
			return status, fmt.Errorf("failed to parse labels for container %s: %w", helpers.SafeIDPrefix(c.ID), err)
		}

		// If a running deployment exists, only count those with a lower deploymentID.
		if runningDeploymentID != "" {
			if c.State != "running" && c.State != "restarting" && labels.DeploymentID < runningDeploymentID {
				availableRollbackContainerIDs = append(availableRollbackContainerIDs, helpers.SafeIDPrefix(c.ID))
			}
		} else {
			// No running container found: decide how to handle rollback.
			// For example, count all stopped containers as available for rollback.
			if c.State != "running" && c.State != "restarting" {
				availableRollbackContainerIDs = append(availableRollbackContainerIDs, helpers.SafeIDPrefix(c.ID))
			}
		}
	}

	formattedDomains := make([]string, 0, len(appConfig.Domains))
	for _, d := range appConfig.Domains {
		if len(d.Aliases) == 0 {
			formattedDomains = append(formattedDomains, fmt.Sprintf("  %s", d.Canonical))
		} else {
			aliases := strings.Join(d.Aliases, ", ")
			aliasStyle := lipgloss.NewStyle().Foreground(ui.LightGray).Italic(true)
			styledAliases := aliasStyle.Render(fmt.Sprintf("<- %s", aliases))
			formattedDomains = append(formattedDomains, fmt.Sprintf("  %s %s", d.Canonical, styledAliases))
		}
	}

	// Build environment variables output.
	var formattedEnvVars []string
	for _, ev := range appConfig.Env {
		var val string
		if ev.Value != nil {
			val = *ev.Value
		} else if ev.SecretName != nil {
			// If there's a secret reference, simulate the "SECRET:" prefix.
			val = "SECRET:" + *ev.SecretName
		}
		if strings.HasPrefix(val, "SECRET:") {
			secretName := strings.TrimPrefix(val, "SECRET:")
			formattedEnvVars = append(formattedEnvVars, fmt.Sprintf("  %s: loaded from secret (%s)", ev.Name, secretName))
		} else {
			formattedEnvVars = append(formattedEnvVars, fmt.Sprintf("  %s: %s", ev.Name, val))
		}
	}

	var rollbackMsg string
	if len(availableRollbackContainerIDs) == 1 {
		rollbackMsg = "1 rollback container available"
	} else {
		rollbackMsg = fmt.Sprintf("%d rollback containers available", len(availableRollbackContainerIDs))
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
		state:                         state,
		runningContainerIDs:           runningContainerIDs,
		availableRollbackContainerIDs: availableRollbackContainerIDs,
		domains:                       appConfig.Domains,
		formattedDomains:              formattedDomains,
		envVars:                       appConfig.Env,
		formattedEnvVars:              formattedEnvVars,
		formattedOutput:               formattedOutput,
	}

	return status, nil
}

func showAppStatusDetailed(ctx context.Context, cli *client.Client, appConfig *config.AppConfig) error {

	status, err := getInitialStatus(ctx, cli, appConfig)
	if err != nil {
		return err
	}

	ui.Section(fmt.Sprintf("%s (detailed status)", appConfig.Name), status.formattedOutput)

	return nil
}

func showAppStatus(ctx context.Context, cli *client.Client, appConfig *config.AppConfig) error {
	status, err := getInitialStatus(ctx, cli, appConfig)
	if err != nil {
		return err
	}

	ui.Section(fmt.Sprintf("%s (detailed status)", appConfig.Name), status.formattedOutput)

	return nil
}
