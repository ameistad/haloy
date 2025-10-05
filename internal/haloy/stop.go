package haloy

import (
	"context"
	"sync"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/appconfigloader"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func StopAppCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag string
	var removeContainersFlag bool

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop an application's running containers",
		Long:  "Stop all running containers for an application using a haloy configuration file.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			ctx := cmd.Context()
			if serverFlag != "" {
				stopApp(ctx, nil, serverFlag, "", removeContainersFlag)
			} else {
				targets, _, err := appconfigloader.Load(ctx, *configPath, flags.targets, flags.all)
				if err != nil {
					ui.Error("%v", err)
					return
				}

				var wg sync.WaitGroup
				for _, target := range targets {
					wg.Add(1)
					go func(target appconfigloader.AppConfigTarget) {
						defer wg.Done()
						stopApp(ctx, &target.ResolvedAppConfig, target.ResolvedAppConfig.Server, target.ResolvedAppConfig.Name, removeContainersFlag)
					}(target)
				}

				wg.Wait()
			}
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Stop app on specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Stop app on all targets")
	cmd.Flags().BoolVarP(&removeContainersFlag, "remove-containers", "r", false, "Remove containers after stopping them")

	return cmd
}

func stopApp(ctx context.Context, appConfig *config.AppConfig, targetServer, appName string, removeContainers bool) {
	token, err := getToken(appConfig, targetServer)
	if err != nil {
		ui.Error("%v", err)
		return
	}

	ui.Info("Stopping application: %s using server %s", appName, targetServer)

	api, err := apiclient.New(targetServer, token)
	if err != nil {
		ui.Error("Failed to create API client: %v", err)
		return
	}
	response, err := api.StopApp(ctx, appName, removeContainers)
	if err != nil {
		ui.Error("Failed to stop app: %v", err)
		return
	}

	ui.Success("%s", response.Message)
}
