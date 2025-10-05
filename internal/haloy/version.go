package haloy

import (
	"context"
	"sync"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/appconfigloader"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func VersionCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var serverFlag string

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the current version of haloyd and HAProxy",
		Long:  "Display the current version of haloyd and the HAProxy version it is using.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			if serverFlag != "" {
				getVersion(context.Background(), nil, serverFlag)
			} else {
				ctx := cmd.Context()
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
						getVersion(ctx, &target.ResolvedAppConfig, target.ResolvedAppConfig.Server)
					}(target)
				}

				wg.Wait()
			}
		},
	}
	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Get version for specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Get version for all targets")
	return cmd
}

func getVersion(ctx context.Context, appConfig *config.AppConfig, targetServer string) {
	token, err := getToken(appConfig, targetServer)
	if err != nil {
		ui.Error("%v", err)
		return
	}
	ui.Info("Getting version using server %s", targetServer)

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
