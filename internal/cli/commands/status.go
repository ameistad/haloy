package commands

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/helpers"
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

func showAppStatus(app *config.AppConfig) error {
	// Get container status and ID.
	containerID, err := getContainerID(app.Name)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}

	status, err := getContainerStatus(app.Name)
	if err != nil {
		return fmt.Errorf("failed to get container status: %w", err)
	}

	// Build domains output.
	var domainLines []string
	for _, d := range app.Domains {
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
	for _, ev := range app.Env {
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
	fmt.Printf("%s: %s\n", label("App"), app.Name)
	fmt.Printf("%s: %s\n", label("Status"), success(status))
	fmt.Printf("%s:\n%s\n", label("Domains"), domainsStr)
	fmt.Printf("%s: %s\n", label("Container ID"), containerID)
	if envStr != "" {
		fmt.Printf("%s:\n%s\n", label("Environment Variables"), envStr)
	}
	fmt.Println(header("-------------------------------------------------"))
	return nil
}

// getContainerID returns the container ID for an app by filtering on the image ancestor.
func getContainerID(appName string) (string, error) {
	cmd := exec.Command("docker", "ps", "--filter", fmt.Sprintf("ancestor=%s:latest", appName), "--format", "{{.ID}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// getContainerStatus returns the container status for an app.
func getContainerStatus(appName string) (string, error) {
	cmd := exec.Command("docker", "ps", "--filter", fmt.Sprintf("ancestor=%s:latest", appName), "--format", "{{.Status}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	status := strings.TrimSpace(string(output))
	if status == "" {
		return "Not running", nil
	}
	return status, nil
}
