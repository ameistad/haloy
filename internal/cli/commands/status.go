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
			context, cancel := context.WithTimeout(context.Background(), showStatusTimeout)
			defer cancel()
			dockerClient, err := docker.NewClient(context)
			if err != nil {
				ui.Error("Error: %s", err)
				return
			}
			defer dockerClient.Close()

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
					if err := showAppStatus(dockerClient, context, &configFile.Apps[i]); err != nil {
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

			if err := showAppStatusDetailed(dockerClient, context, appConfig); err != nil {
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
	state            string
	containerIDs     []string
	domains          []config.Domain
	formattedDomains []string
	envVars          []config.EnvVar
	formattedEnvVars []string
}

func getInitialStatus(dockerClient *client.Client, context context.Context, appConfig *config.AppConfig) (initialStatus, error) {
	status := initialStatus{}
	filtersArgs := filters.NewArgs()
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelRole, config.AppLabelRole))
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, appConfig.Name))

	containers, err := dockerClient.ContainerList(context, container.ListOptions{
		Filters: filtersArgs,
		All:     false,
	})
	if err != nil {
		return status, fmt.Errorf("failed to get container for app %s: %w", appConfig.Name, err)
	}

	state := "Not running"
	containerIDs := make([]string, len(containers))
	if len(containers) > 0 {
		for i, c := range containers {
			if c.State == "running" || c.State == "restarting" {
				containerIDs[i] = helpers.SafeIDPrefix(c.ID)
			}
		}

		for _, c := range containers {
			switch c.State {
			case "running":
				state = "Running"
			case "restarting":
				state = "Restarting"
			case "exited":
				state = "Exited"
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

	status = initialStatus{
		state:            state,
		containerIDs:     containerIDs,
		domains:          appConfig.Domains,
		formattedDomains: formattedDomains,
		envVars:          appConfig.Env,
		formattedEnvVars: formattedEnvVars,
	}

	return status, nil
}

func showAppStatusDetailed(dockerClient *client.Client, context context.Context, appConfig *config.AppConfig) error {

	status, err := getInitialStatus(dockerClient, context, appConfig)
	if err != nil {
		return err
	}

	output := []string{
		fmt.Sprintf("State: %s", status.state),
		fmt.Sprintf("Domains:\n%s", strings.Join(status.formattedDomains, "\n")),
		fmt.Sprintf("Container IDs: %s", strings.Join(status.containerIDs, ", ")),
	}

	if len(status.formattedEnvVars) > 0 {
		output = append(output, fmt.Sprintf("Environment Variables:\n%s", strings.Join(status.formattedEnvVars, "\n")))
	}
	// Create section
	ui.Section(fmt.Sprintf("%s (detailed status)", appConfig.Name), output)

	return nil
}

func showAppStatus(dockerClient *client.Client, context context.Context, appConfig *config.AppConfig) error {
	status, err := getInitialStatus(dockerClient, context, appConfig)
	if err != nil {
		return err
	}

	output := []string{
		fmt.Sprintf("State: %s", status.state),
		fmt.Sprintf("Domains:\n%s", strings.Join(status.formattedDomains, "\n")),
		fmt.Sprintf("Container IDs: %s", strings.Join(status.containerIDs, ", ")),
	}

	if len(status.formattedEnvVars) > 0 {
		output = append(output, fmt.Sprintf("Environment Variables:\n%s", strings.Join(status.formattedEnvVars, "\n")))
	}
	// Create section
	ui.Section(appConfig.Name, output)

	return nil
}
