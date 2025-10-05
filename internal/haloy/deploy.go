package haloy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/appconfigloader"
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
			targets, baseAppConfig, err := appconfigloader.Load(ctx, *configPath, flags.targets, flags.all)
			if err != nil {
				ui.Error("%v", err)
				return
			}
			deploymentID := createDeploymentID()

			if len(baseAppConfig.GlobalPreDeploy) > 0 {
				for _, hookCmd := range baseAppConfig.GlobalPreDeploy {
					if err := executeHook(hookCmd, getHooksWorkDir(*configPath)); err != nil {
						ui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "GlobalPreDeploy", baseAppConfig.Format), err)
						return
					}
				}
			}

			var wg sync.WaitGroup

			for _, target := range targets {
				wg.Add(1)
				go func(target appconfigloader.AppConfigTarget) {
					defer wg.Done()
					deployTarget(ctx, target, *configPath, deploymentID, noLogsFlag, len(targets) > 1)
				}(target)
			}

			wg.Wait()

			if len(baseAppConfig.GlobalPostDeploy) > 0 {
				for _, hookCmd := range baseAppConfig.GlobalPostDeploy {
					if err := executeHook(hookCmd, getHooksWorkDir(*configPath)); err != nil {
						ui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "GlobalPostDeploy", baseAppConfig.Format), err)
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

func deployTarget(ctx context.Context, target appconfigloader.AppConfigTarget, configPath, deploymentID string, noLogs, showTargetName bool) {
	targetName := target.ResolvedAppConfig.TargetName
	format := target.ResolvedAppConfig.Format
	server := target.ResolvedAppConfig.Server
	preDeploy := target.ResolvedAppConfig.PreDeploy
	postDeploy := target.ResolvedAppConfig.PostDeploy

	prefix := ""
	if showTargetName {
		prefix = lipgloss.NewStyle().Bold(true).Foreground(ui.White).Render(fmt.Sprintf("%s ", targetName))
	}
	pui := &ui.PrefixedUI{Prefix: prefix}

	pui.Info("Deployment started for %s", targetName)

	if len(preDeploy) > 0 {
		for _, hookCmd := range preDeploy {
			if err := executeHook(hookCmd, getHooksWorkDir(configPath)); err != nil {
				pui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "PreDeploy", format), err)
				return
			}
		}
	}

	token, err := getToken(&target.ResolvedAppConfig, server)
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
		RawAppConfig:      target.RawAppConfig,
		ResolvedAppConfig: target.ResolvedAppConfig,
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
			if err := executeHook(hookCmd, getHooksWorkDir(configPath)); err != nil {
				pui.Error("%s hook failed: %v", config.GetFieldNameForFormat(config.AppConfig{}, "PostDeploy", format), err)
			}
		}
	}
}

// executeHook runs a single hook command in the specified working directory.
func executeHook(command string, workDir string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return fmt.Errorf("empty hook command")
	}
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

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
