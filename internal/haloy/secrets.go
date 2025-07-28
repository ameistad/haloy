package haloy

import (
	"context"
	"fmt"
	"strings"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

// SecretsSetCommand encrypts a plain-text value and stores it under the provided key.
func SecretsSetCommand() *cobra.Command {
	var configPath string
	var serverURL string
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

			targetServer, err := getServerURL(serverURL, configPath)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			api := apiclient.New(targetServer)
			err = api.SetSecret(ctx, name, value)
			if err != nil {
				ui.Error("Failed to set secret: %v", err)
				return
			}

			ui.Success("Secret '%s' set successfully", name)

		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file or directory")
	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	return cmd
}

// SecretsListCommand lists all stored secrets in a table.
func SecretsListCommand() *cobra.Command {
	var configPath string
	var serverURL string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all stored secrets",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			targetServer, err := getServerURL(serverURL, configPath)
			if err != nil {
				ui.Error("%v", err)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()
			api := apiclient.New(targetServer)
			response, err := api.SecretsList(ctx)
			if err != nil {
				ui.Error("Failed to list secrets: %v", err)
				return
			}
			secrets := response.Secrets
			if len(secrets) == 0 {
				ui.Info("No secrets found.")
				return
			}

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
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file or directory")
	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	return cmd
}

func SecretsDeleteCommand() *cobra.Command {
	var configPath string
	var serverURL string
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a secret from the server",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]

			targetServer, err := getServerURL(serverURL, configPath)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			api := apiclient.New(targetServer)
			err = api.DeleteSecret(ctx, name)
			if err != nil {
				ui.Error("Failed to delete secret: %v", err)
				return
			}

			ui.Success("Secret '%s' deleted successfully", name)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file or directory")
	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	return cmd
}

func getServerURL(serverURL, configPath string) (string, error) {
	if serverURL != "" {
		ui.Info("Using server URL from command line: %s", serverURL)
		return serverURL, nil
	}

	if configPath == "" {
		configPath = "."
	}

	appConfig, err := config.LoadAndValidateAppConfig(configPath)
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	return appConfig.Server, nil
}

func SecretsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage encrypted secrets on the server",
	}
	cmd.AddCommand(SecretsSetCommand())
	cmd.AddCommand(SecretsListCommand())
	cmd.AddCommand(SecretsDeleteCommand())
	return cmd
}
