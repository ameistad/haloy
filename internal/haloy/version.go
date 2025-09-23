package haloy

import (
	"context"
	"sync"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func VersionCmd(configPath *string) *cobra.Command {
	var serverFlag string
	var targetFlag string
	var allFlag bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the current version of haloyd and HAProxy",
		Long:  "Display the current version of haloyd and the HAProxy version it is using.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			if serverFlag != "" {
				getVersion(nil, serverFlag)
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
						getVersion(&appConfig, target.Config.Server)
					}(target)
				}

				wg.Wait()
			}
		},
	}
	cmd.Flags().StringVarP(&serverFlag, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "Show logs of a specific target")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Show all target logs")

	return cmd
}

func getVersion(appConfig *config.AppConfig, targetServer string) {
	token, err := getToken(appConfig, targetServer)
	if err != nil {
		ui.Error("%v", err)
		return
	}
	ui.Info("Getting version using server %s", targetServer)

	ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
	defer cancel()
	cliVersion := constants.Version
	api, err := apiclient.New(targetServer, token)
	if err != nil {
		ui.Error("Failed to create API client: %v", err)
		return
	}
	response, err := api.Version(ctx)
	if err != nil {
		ui.Error("Failed to get version from API: %v", err)
		return
	}

	if cliVersion == response.Version {
		ui.Success("haloy version %s running with HAProxy version %s", cliVersion, response.HAProxyVersion)
	} else {
		ui.Warn("haloy version %s does not match haloyd (server) version %s", cliVersion, response.Version)
		ui.Warn("HAProxy version: %s", response.HAProxyVersion)
	}
}
