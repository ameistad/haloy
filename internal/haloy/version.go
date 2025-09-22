package haloy

import (
	"context"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func VersionCmd(configPath *string) *cobra.Command {
	var serverURL string

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the current version of haloyd and HAProxy",
		Long:  "Display the current version of haloyd and the HAProxy version it is using.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			appConfig, _, _ := config.LoadAppConfig(*configPath)

			targetServer, err := getServer(appConfig, serverURL)
			if err != nil {
				ui.Error("%v", err)
				return
			}

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
		},
	}
	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	return cmd
}
