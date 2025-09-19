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
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func DeployAppCmd(configPath *string) *cobra.Command {
	var noLogsFlag bool
	var targetFlag string
	var allFlag bool

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy an application",
		Long:  "Deploy an application using a haloy configuration file.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			appConfig, format, err := config.LoadAppConfig(*configPath)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			targets, err := expandTargets(appConfig, targetFlag, allFlag)
			if err != nil {
				ui.Error("Failed to process deployment targets: %v", err)
				return
			}

			deploymentID := createDeploymentID()

			var wg sync.WaitGroup

			for _, target := range targets {
				wg.Add(1)
				go func(target ExpandedTarget) {
					defer wg.Done()
					deployTarget(target, *configPath, deploymentID, format, noLogsFlag, len(targets) > 1)
				}(target)
			}

			wg.Wait()
		},
	}

	cmd.Flags().BoolVar(&noLogsFlag, "no-logs", false, "Don't stream deployment logs")
	cmd.Flags().StringVarP(&targetFlag, "target", "t", "", "Deploy to a specific target")
	cmd.Flags().BoolVarP(&allFlag, "all", "a", false, "Deploy to all targets")

	return cmd
}

func deployTarget(target ExpandedTarget, configPath, deploymentID, format string, noLogs, showTargetName bool) {
	prefix := ""
	if showTargetName {
		prefix = lipgloss.NewStyle().Bold(true).Foreground(ui.White).Render(fmt.Sprintf("%s ", target.TargetName))
	}
	pui := &ui.PrefixedUI{Prefix: prefix}

	pui.Info("Deployment started for %s", target.Config.Name)

	if len(target.Config.PreDeploy) > 0 {
		for _, hookCmd := range target.Config.PreDeploy {
			if err := executeHook(hookCmd, getHooksWorkDir(configPath)); err != nil {
				pui.Error("Pre-deploy hook failed: %v", err)
				return
			}
		}
	}

	targetServer, err := getServer(target.Config, "")
	if err != nil {
		pui.Error("%v", err)
		return
	}

	token, err := getToken(target.Config, targetServer)
	if err != nil {
		pui.Error("%v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
	defer cancel()

	// Send the deploy request
	api := apiclient.New(targetServer, token)
	request := apitypes.DeployRequest{AppConfig: target.Config, DeploymentID: deploymentID, ConfigFormat: format}
	err = api.Post(ctx, "deploy", request, nil)
	if err != nil {
		pui.Error("Deployment request failed: %v", err)
		return
	}

	if !noLogs {
		// No timout for streaming logs
		streamCtx, streamCancel := context.WithCancel(context.Background())
		defer streamCancel()

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

		api.Stream(streamCtx, streamPath, streamHandler)
	}

	if len(target.Config.PostDeploy) > 0 {
		for _, hookCmd := range target.Config.PostDeploy {
			if err := executeHook(hookCmd, getHooksWorkDir(configPath)); err != nil {
				ui.Warn("Post-deploy hook failed: %v", err)
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
