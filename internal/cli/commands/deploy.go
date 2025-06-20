package commands

import (
	"context"
	"fmt"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/docker"
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
			ctx, cancel := context.WithTimeout(context.Background(), deploy.DefaultDeployTimeout)
			defer cancel()

			cli, err := docker.NewClient(ctx)
			if err != nil {
				ui.Error("Failed to create Docker client: %v", err)
				return
			}
			defer cli.Close()

			imageTag, err := deploy.GetImage(ctx, cli, appConfig)
			if err != nil {
				ui.Error("Failed to get image for %q: %v\n", appName, err)
				return
			}

			if err := deploy.DeployApp(ctx, cli, appConfig, imageTag); err != nil {
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

			ctx, cancel := context.WithTimeout(context.Background(), deploy.DefaultDeployTimeout)
			defer cancel()

			cli, err := docker.NewClient(ctx)
			if err != nil {
				ui.Error("Failed to create Docker client: %v", err)
				return
			}
			defer cli.Close()

			for i := range configFile.Apps {
				app := configFile.Apps[i]
				appConfig := &app
				imageTag, err := deploy.GetImage(ctx, cli, appConfig)
				if err != nil {
					ui.Error("Failed to get image for %q: %v\n", appConfig.Name, err)
					continue
				}
				if err := deploy.DeployApp(ctx, cli, appConfig, imageTag); err != nil {
					ui.Error("Failed to deploy %q: %v\n", app.Name, err)
				}
			}
		},
	}
	return deployAllCmd
}
