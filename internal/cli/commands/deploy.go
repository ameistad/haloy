package commands

import (
	"fmt"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploy"
	"github.com/spf13/cobra"
)

func DeployAppCmd() *cobra.Command {
	deployAppCmd := &cobra.Command{
		Use:   "deploy <app-name>",
		Short: "Deploy an application",
		Long:  `Deploy a single application by name`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("app deploy requires exactly one argument: the app name (e.g., 'haloy app deploy my-app')")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			appName := args[0]
			appConfig, err := config.AppConfigByName(appName)
			if err != nil {
				return fmt.Errorf("failed to get configuration for %q: %w", appName, err)
			}

			return deploy.DeployApp(appConfig)
		},
	}
	return deployAppCmd
}

func DeployAllCmd() *cobra.Command {
	deployAllCmd := &cobra.Command{
		Use:   "deploy-all",
		Short: "Deploy all applications",
		Long:  `Deploy all applications defined in the configuration file.`,
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

			// Iterate over all apps using indices to take a pointer reference.
			for i := range configFile.Apps {
				// Create a copy of the app config
				app := configFile.Apps[i]
				appConfig := &app
				fmt.Printf("Deploying app '%s'...\n", appConfig.Name)
				if err := deploy.DeployApp(appConfig); err != nil {
					fmt.Printf("Failed to deploy app '%s': %v\n", appConfig.Name, err)
				} else {
					fmt.Printf("Successfully deployed app '%s'.\n", appConfig.Name)
				}
			}
			return nil
		},
	}
	return deployAllCmd
}
