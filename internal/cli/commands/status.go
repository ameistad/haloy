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
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/fatih/color"
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
					if err := showAppStatus(&configFile.Apps[i]); err != nil {
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

			if err := showAppStatus(appConfig); err != nil {
				ui.Error("Error: %s", err)
			}

		},
	}
	return statusAppCmd
}

const (
	showStatusTimeout = 5 * time.Second
)

func showAppStatus(appConfig *config.AppConfig) error {

	ctx, cancel := context.WithTimeout(context.Background(), showStatusTimeout)
	defer cancel()
	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer dockerClient.Close()

	filtersArgs := filters.NewArgs()
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelRole, config.AppLabelRole))
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, appConfig.Name))

	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filtersArgs,
		All:     false,
	})
	if err != nil {
		return fmt.Errorf("failed to get container for app %s: %w", appConfig.Name, err)
	}

	state := "Not running"
	runningContainerIdsStr := "None"
	if len(containers) > 0 {
		runningContainerIds := make([]string, len(containers))
		for i, c := range containers {
			if c.State == "running" || c.State == "restarting" {
				runningContainerIds[i] = helpers.SafeIDPrefix(c.ID)
			}
		}

		runningContainerIdsStr = strings.Join(runningContainerIds, ", ")

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

	// Build domains output.
	var domainLines []string
	for _, d := range appConfig.Domains {
		// Canonical domain.
		ip, err := helpers.GetARecord(d.Canonical)
		if err != nil {
			domainLines = append(domainLines, fmt.Sprintf("  - %s -> %s", d.Canonical, color.RedString("no A record found")))
		} else {
			domainLines = append(domainLines, fmt.Sprintf("  - %s -> %s", d.Canonical, ip.String()))
		}

		// Aliases, if any.
		for _, alias := range d.Aliases {
			ipAlias, err := helpers.GetARecord(alias)
			if err != nil {
				domainLines = append(domainLines, fmt.Sprintf("  - %s -> %s", alias, color.RedString("no A record found")))
			} else {
				domainLines = append(domainLines, fmt.Sprintf("  - %s -> %s", alias, ipAlias.String()))
			}
		}
	}
	domainsStr := strings.Join(domainLines, "\n")

	// Build environment variables output.
	var envLines []string
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
			envLines = append(envLines, fmt.Sprintf("  %s: loaded from secret (%s)", ev.Name, secretName))
		} else {
			envLines = append(envLines, fmt.Sprintf("  %s: %s", ev.Name, val))
		}
	}
	envStr := strings.Join(envLines, "\n")

	output := []string{
		fmt.Sprintf("State: %s", state),
		fmt.Sprintf("Domains:\n%s", domainsStr),
		fmt.Sprintf("Container IDs: %s", runningContainerIdsStr),
	}

	if envStr != "" {
		output = append(output, fmt.Sprintf("Environment Variables:\n%s", envStr))
	}
	// Create section
	ui.Section(appConfig.Name, output)

	return nil
}
