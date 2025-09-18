package haloy

import (
	"context"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func StopAppCmd(configPath *string) *cobra.Command {
	var serverURL string
	var removeContainersFlag bool

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop an application's running containers",
		Long:  "Stop all running containers for an application using a haloy configuration file.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			appConfig, _, err := config.LoadAppConfig(*configPath)
			if err != nil {
				ui.Error("Failed to load config: %v", err)
				return
			}

			targetServer := appConfig.Server
			if serverURL != "" {
				targetServer = serverURL
			}

			token, err := getToken(appConfig, targetServer)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			ui.Info("Stopping application: %s using server %s", appConfig.Name, targetServer)
			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			api := apiclient.New(targetServer, token)
			response, err := api.StopApp(ctx, appConfig.Name, removeContainersFlag)
			if err != nil {
				ui.Error("Failed to stop app: %v", err)
				return
			}

			ui.Success("%s", response.Message)
		},
	}

	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().BoolVarP(&removeContainersFlag, "remove-containers", "r", false, "Remove containers after stopping them")

	return cmd
}
