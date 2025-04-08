package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

func StatusAppCmd() *cobra.Command {
	statusAppCmd := &cobra.Command{
		Use:   "status <app-name>",
		Short: "Get the status of an application",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			appName := args[0]
			appConfig, err := config.AppConfigByName(appName)
			if err != nil {
				return err
			}

			if err := showAppStatus(appConfig); err != nil {
				return err
			}

			return nil
		},
	}
	return statusAppCmd
}

func StatusAllCmd() *cobra.Command {
	statusAllCmd := &cobra.Command{
		Use:   "status-all",
		Short: "Get the status of all applications in the configuration file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {

			configFilePath, err := config.ConfigFilePath()
			if err != nil {
				return err
			}
			configFile, err := config.LoadAndValidateConfig(configFilePath)
			if err != nil {
				return fmt.Errorf("configuration error: %w", err)
			}

			// Show status for each app.
			for i := range configFile.Apps {
				if err := showAppStatus(&configFile.Apps[i]); err != nil {
					return err
				}
			}
			return nil
		},
	}
	return statusAllCmd
}

const (
	showStatusTimeout = 5 * time.Minute
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

	if len(containers) == 0 {
		return fmt.Errorf("no running container found for app %s", appConfig.Name)
	}

	runningContainerIds := make([]string, len(containers))
	for i, c := range containers {
		if c.State == "running" || c.State == "restarting" {
			runningContainerIds[i] = c.ID[:12]
		}
	}

	runningContainerIdsStr := strings.Join(runningContainerIds, ", ")

	state := "Not running"
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

	// Define color functions.
	header := color.New(color.Bold, color.FgCyan).SprintFunc()
	label := color.New(color.FgYellow).SprintFunc()
	success := color.New(color.FgGreen).SprintFunc()

	// Display structured output.
	fmt.Println(header("-------------------------------------------------"))
	fmt.Printf("%s: %s\n", label("App"), appConfig.Name)
	fmt.Printf("%s: %s\n", label("State"), success(state))
	fmt.Printf("%s:\n%s\n", label("Domains"), domainsStr)
	fmt.Printf("%s: %s\n", label("Container IDs"), runningContainerIdsStr)
	if envStr != "" {
		fmt.Printf("%s:\n%s\n", label("Environment Variables"), envStr)
	}
	fmt.Println(header("-------------------------------------------------"))
	return nil
}
