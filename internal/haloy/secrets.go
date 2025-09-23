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
	"github.com/spf13/cobra"
)

// SecretsSetCmd encrypts a plain-text value and stores it under the provided key.
func SecretsSetCmd(configPath *string) *cobra.Command {
	var serverFlag string
	var targetFlag string
	var allFlag bool

	cmd := &cobra.Command{
		Use:     "set <name> <value>",
		Short:   "Encrypt a plain-text value and store it under <name>",
		Example: "  haloy secrets set MY_SECRET supersecretvalue\n  haloy secrets set DB_PASSWORD 'p@ssw0rd!'",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				ui.Error("Error: You must provide a <name> and a <value> to store a secret.\n")
				ui.Info("%s", cmd.UsageString())
				return fmt.Errorf("requires at least 2 arg(s), only received %d", len(args))
			}
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			value := strings.Join(args[1:], " ")

			if serverFlag != "" {
				setSecret(nil, serverFlag, name, value)
			} else {
				appConfig, _, err := config.LoadAppConfig(*configPath)
				if err != nil {
					ui.Error("%v", err)
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
						setSecret(&appConfig, target.Config.Server, name, value)
					}(target)
				}

				wg.Wait()
			}
		},
	}
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "Set secret for a specific target")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Set secret to all targets")
	return cmd
}

func setSecret(appConfig *config.AppConfig, targetServer, name, value string) {
	token, err := getToken(appConfig, targetServer)
	if err != nil {
		ui.Error("%v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
	defer cancel()

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		ui.Error("Failed to create API client: %v", err)
		return
	}
	err = api.SetSecret(ctx, name, value)
	if err != nil {
		ui.Error("Failed to set secret: %v", err)
		return
	}

	ui.Success("Secret '%s' set successfully on %s", name, targetServer)
}

func listSecrets(appConfig *config.AppConfig, targetServer string) {
	token, err := getToken(appConfig, targetServer)
	if err != nil {
		ui.Error("%v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
	defer cancel()
	api, err := apiclient.New(targetServer, token)
	if err != nil {
		ui.Error("Failed to create API client: %v", err)
		return
	}
	response, err := api.SecretsList(ctx)
	if err != nil {
		ui.Error("Failed to list secrets: %v", err)
		return
	}
	secrets := response.Secrets
	if len(secrets) == 0 {
		ui.Info("No secrets found on %s.", targetServer)
		return
	}

	ui.Info("Secrets stored on %s:", targetServer)
	headers := []string{"NAME", "DIGEST", "DATE"}
	rows := make([][]string, 0, len(secrets))
	for _, secret := range secrets {
		date, err := helpers.FormatDateString(secret.UpdatedAt)
		if err != nil {
			date = secret.UpdatedAt // Fallback to raw date if formatting fails
		}
		rows = append(rows, []string{secret.Name, secret.DigestValue, date})
	}

	ui.Table(headers, rows)
}

func deleteSecret(appConfig *config.AppConfig, targetServer, name string) {
	token, err := getToken(appConfig, targetServer)
	if err != nil {
		ui.Error("%v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
	defer cancel()

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		ui.Error("Failed to create API client: %v", err)
		return
	}
	err = api.DeleteSecret(ctx, name)
	if err != nil {
		ui.Error("Failed to delete secret: %v", err)
		return
	}

	ui.Success("Secret '%s' deleted successfully on %s", name, targetServer)
}

// SecretsListCmd lists all stored secrets in a table.
func SecretsListCmd(configPath *string) *cobra.Command {
	var serverFlag string
	var targetFlag string
	var allFlag bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all stored secrets",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			if serverFlag != "" {
				listSecrets(nil, serverFlag)
			} else {
				appConfig, _, err := config.LoadAppConfig(*configPath)
				if err != nil {
					ui.Error("%v", err)
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
						listSecrets(&appConfig, target.Config.Server)
					}(target)
				}

				wg.Wait()
			}
		},
	}
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "List secrets for a specific target")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "List secrets from all targets")
	return cmd
}

func SecretsDeleteCmd(configPath *string) *cobra.Command {
	var serverFlag string
	var targetFlag string
	var allFlag bool

	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a secret from the server",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]

			if serverFlag != "" {
				deleteSecret(nil, serverFlag, name)
			} else {
				appConfig, _, err := config.LoadAppConfig(*configPath)
				if err != nil {
					ui.Error("%v", err)
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
						deleteSecret(&appConfig, target.Config.Server, name)
					}(target)
				}

				wg.Wait()
			}
		},
	}
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "Delete secret from a specific target")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Delete secret from all targets")
	return cmd
}

func SecretsCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage encrypted secrets on the server",
	}
	cmd.AddCommand(SecretsSetCmd(configPath))
	cmd.AddCommand(SecretsListCmd(configPath))
	cmd.AddCommand(SecretsDeleteCmd(configPath))
	return cmd
}
