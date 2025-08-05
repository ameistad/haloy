package haloy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

func DeployAppCmd() *cobra.Command {
	var configPath string
	var serverURL string
	var noLogs bool

	cmd := &cobra.Command{
		Use:   "deploy [config-path]",
		Short: "Deploy an application",
		Long: `Deploy an application using a haloy configuration file.

The path can be:
- A directory containing haloy.json, haloy.yaml, haloy.yml, or haloy.toml
- A full path to a config file with supported extension (.json, .yaml, .yml, .toml)
- A relative path to either of the above

If no path is provided, the current directory is used.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// Determine config path
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

			if len(appConfig.PreDeploy) > 0 {
				for _, hookCmd := range appConfig.PreDeploy {
					if err := executeHook(hookCmd, getHooksWorkDir(configPath)); err != nil {
						ui.Error("Pre-deploy hook failed: %v", err)
						return
					}
				}
			}

			targetServer := appConfig.Server
			if serverURL != "" {
				targetServer = serverURL
			}

			ui.Info("Starting deployment for application: %s using server %s", appConfig.Name, targetServer)
			ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
			defer cancel()

			api := apiclient.New(targetServer)
			resp, err := api.Deploy(ctx, *appConfig)
			if err != nil {
				ui.Error("Deployment request failed: %v", err)
				return
			}
			if resp == nil {
				ui.Error("No response from server")
				return
			}

			if !noLogs {
				// No timout for streaming logs
				streamCtx, streamCancel := context.WithCancel(context.Background())
				defer streamCancel()

				// Stream deployment logs using the APIClient
				if err := api.StreamDeploymentLogs(streamCtx, resp.DeploymentID); err != nil {
					ui.Warn("Failed to stream deployment logs: %v", err)
				}
			}

			if len(appConfig.PostDeploy) > 0 {
				for _, hookCmd := range appConfig.PostDeploy {
					if err := executeHook(hookCmd, getHooksWorkDir(configPath)); err != nil {
						ui.Warn("Post-deploy hook failed: %v", err)
					}
				}
			}
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file or directory")
	cmd.Flags().StringVarP(&serverURL, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream deployment logs")

	return cmd
}

// executeHook runs a single hook command in the specified working directory.
func executeHook(command string, workDir string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty hook command")
	}
	prog := parts[0]
	args := parts[1:]

	cmd := exec.Command(prog, args...)
	cmd.Dir = workDir      // Set the working directory for the command
	cmd.Stdout = os.Stdout // Stream stdout to the user's terminal
	cmd.Stderr = os.Stderr // Stream stderr to the user's terminal

	return cmd.Run()
}

func getHooksWorkDir(configPath string) string {
	workDir := "."
	if configPath != "." {
		// If a specific config path was provided, use its directory
		if stat, err := os.Stat(configPath); err == nil {
			if stat.IsDir() {
				workDir = configPath
			} else {
				workDir = filepath.Dir(configPath)
			}
		}
	}
	return workDir
}
