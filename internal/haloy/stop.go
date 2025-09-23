package haloy

import (
	"context"
	"sync"

	"github.com/ameistad/haloy/internal/apiclient"
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
			if serverFlag != "" {
				stopApp(nil, serverFlag, "", removeContainersFlag)
			} else {
				appConfig, _, err := config.LoadAppConfig(*configPath)
				if err != nil {
					ui.Error("Failed to load config: %v", err)
					return
				}
				targets, err := expandTargets(appConfig, flags.targets, flags.all)
				if err != nil {
					ui.Error("Failed to process deployment targets: %v", err)
					return
				}

				var wg sync.WaitGroup
				for _, target := range targets {
					wg.Add(1)
					go func(target ExpandedTarget) {
						defer wg.Done()
						stopApp(&appConfig, target.Config.Server, appConfig.Name, removeContainersFlag)
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

func stopApp(appConfig *config.AppConfig, targetServer, appName string, removeContainers bool) {
	token, err := getToken(appConfig, targetServer)
	if err != nil {
		ui.Error("%v", err)
		return
	}

	ui.Info("Stopping application: %s using server %s", appName, targetServer)
	ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
	defer cancel()

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
