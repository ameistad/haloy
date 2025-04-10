package commands

import (
	"fmt"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/ui"
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
		Run: func(cmd *cobra.Command, args []string) {
			appName := args[0]
			appConfig, err := config.AppConfigByName(appName)
			if err != nil {
				ui.Error("Failed to get configuration for %q: %v\n", appName, err)
				return
			}

			if err := deploy.DeployApp(appConfig); err != nil {
				ui.Error("Failed to deploy %q: %v\n", appName, err)
			}
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
		Run: func(cmd *cobra.Command, args []string) {
			configFilePath, err := config.ConfigFilePath()
			if err != nil {
				ui.Error("Failed to determine config file path: %v\n", err)
				return
			}
			configFile, err := config.LoadAndValidateConfig(configFilePath)
			if err != nil {
				ui.Error("Failed to load configuration file: %v\n", err)
				return
			}

			for i := range configFile.Apps {
				app := configFile.Apps[i]
				appConfig := &app
				if err := deploy.DeployApp(appConfig); err != nil {
					ui.Error("Failed to deploy %q: %v\n", app.Name, err)
				}
			}
		},
	}
	return deployAllCmd
}
