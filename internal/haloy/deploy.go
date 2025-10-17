package haloy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/appconfigloader"
	"github.com/ameistad/haloy/internal/cmdexec"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func DeployAppCmd(configPath *string, flags *appCmdFlags) *cobra.Command {
	var noLogsFlag bool

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an application",
		Long:  "Deploy an application using a haloy configuration file.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			ctx := cmd.Context()

			rawAppConfig, err := appconfigloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			resolvedAppConfig, err := appconfigloader.ResolveSecrets(ctx, rawAppConfig)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			rawTargets, err := appconfigloader.ResolveTargets(rawAppConfig)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			resolvedTargets, err := appconfigloader.ResolveTargets(resolvedAppConfig)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			if len(rawTargets) != len(resolvedTargets) {
				ui.Error("Mismatch between raw targets (%d) and resolved targets (%d). This indicates a configuration processing error.", len(rawTargets), len(resolvedTargets))
				return
			}

			builds, _ := ResolveImageBuilds(resolvedTargets)
			if len(builds) > 0 {
				for imageRef, image := range builds {
					if err := BuildImage(ctx, imageRef, image, *configPath); err != nil {
						ui.Error("%v", err)
						return
					}
				}
			}

			deploymentID := createDeploymentID()

			if len(rawAppConfig.GlobalPreDeploy) > 0 {
				for _, hookCmd := range rawAppConfig.GlobalPreDeploy {
					if err := cmdexec.RunCommand(ctx, hookCmd, getHooksWorkDir(*configPath)); err != nil {
						ui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "GlobalPreDeploy", rawAppConfig.Format), err)
						return
					}
				}
			}

			var wg sync.WaitGroup
			for i, rawTarget := range rawTargets {
				resolvedTarget := resolvedTargets[i]

				wg.Add(1)
				go func(rawTarget config.AppConfig) {
					defer wg.Done()

					deployTarget(
						ctx,
						rawTarget,
						resolvedTarget,
						*configPath,
						deploymentID,
						noLogsFlag,
						len(rawTargets) > 1,
					)
				}(rawTarget)
			}

			wg.Wait()

			if len(rawAppConfig.GlobalPostDeploy) > 0 {
				for _, hookCmd := range rawAppConfig.GlobalPostDeploy {
					if err := cmdexec.RunCommand(ctx, hookCmd, getHooksWorkDir(*configPath)); err != nil {
						ui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "GlobalPostDeploy", rawAppConfig.Format), err)
						return
					}
				}
			}
		},
	}

	cmd.Flags().StringVarP(&flags.configPath, "config", "c", "", "Path to config file or directory (default: .)")
	cmd.Flags().StringSliceVarP(&flags.targets, "targets", "t", nil, "Deploy to a specific targets (comma-separated)")
	cmd.Flags().BoolVarP(&flags.all, "all", "a", false, "Deploy to all targets")

	return cmd
}

func deployTarget(ctx context.Context, rawAppConfig, resolvedAppConfig config.AppConfig, configPath, deploymentID string, noLogs, showTargetName bool) {
	targetName := rawAppConfig.TargetName
	format := rawAppConfig.Format
	server := rawAppConfig.Server
	preDeploy := rawAppConfig.PreDeploy
	postDeploy := rawAppConfig.PostDeploy

	prefix := ""
	if showTargetName {
		prefix = lipgloss.NewStyle().Bold(true).Foreground(ui.White).Render(fmt.Sprintf("%s ", targetName))
	}
	pui := &ui.PrefixedUI{Prefix: prefix}

	pui.Info("Deployment started for %s", rawAppConfig.Name)

	if len(preDeploy) > 0 {
		for _, hookCmd := range preDeploy {
			if err := cmdexec.RunCommand(ctx, hookCmd, getHooksWorkDir(configPath)); err != nil {
				pui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "PreDeploy", format), err)
				return
			}
		}
	}

	token, err := getToken(&resolvedAppConfig, server)
	if err != nil {
		pui.Error("%v", err)
		return
	}

	// Send the deploy request
	api, err := apiclient.New(server, token)
	if err != nil {
		pui.Error("Failed to create API client: %v", err)
		return
	}

	request := apitypes.DeployRequest{
		RawAppConfig:      rawAppConfig,
		ResolvedAppConfig: resolvedAppConfig,
		DeploymentID:      deploymentID,
	}
	err = api.Post(ctx, "deploy", request, nil)
	if err != nil {
		pui.Error("Deployment request failed: %v", err)
		return
	}

	if !noLogs {
		streamPath := fmt.Sprintf("deploy/%s/logs", deploymentID)

		streamHandler := func(data string) bool {
			var logEntry logging.LogEntry
			if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
				pui.Error("failed to ummarshal json: %v", err)
				return false // we don't stop on errors.
			}

			ui.DisplayLogEntry(logEntry, prefix)

			// If deployment is complete we'll return true to signal stream should stop
			return logEntry.IsDeploymentComplete
		}

		api.Stream(ctx, streamPath, streamHandler)
	}

	if len(postDeploy) > 0 {
		for _, hookCmd := range postDeploy {
			if err := cmdexec.RunCommand(ctx, hookCmd, getHooksWorkDir(configPath)); err != nil {
				pui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "PostDeploy", format), err)
			}
		}
	}
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
