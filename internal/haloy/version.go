package haloy

import (
	"context"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func VersionCmd() *cobra.Command {
	var configPath string
	var serverURL string

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the version of the haloy manager and HAProxy",
		Long:  "Display the current version of the haloy manager and the HAProxy version it is using.",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) > 0 {
				configPath = args[0]
			} else if configPath == "" {
				configPath = "."
			}

			appConfig, err := config.LoadAndValidateAppConfig(configPath)
			if err != nil {
				ui.Error("Failed to load config: %v", err)
				return
			}
			targetServer := appConfig.Server
			if serverURL != "" {
				targetServer = serverURL
			}

			ui.Info("Getting version using server %s", targetServer)
			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()
			cliVersion := constants.Version
			api := apiclient.New(targetServer)
			response, err := api.Version(ctx)
			if err != nil {
				ui.Error("Failed to get version from API: %v", err)
				return
			}

			if cliVersion == response.Version {
				ui.Success("haloy version %s running with HAProxy version %s", cliVersion, response.HAProxyVersion)
			} else {
				ui.Warn("haloy version %s does not match server version %s", cliVersion, response.Version)
				ui.Warn("HAProxy version: %s", response.HAProxyVersion)
			}
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to haloy config file or directory")
	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	return cmd
}
