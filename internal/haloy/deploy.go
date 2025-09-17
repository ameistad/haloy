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

func DeployAppCmd() *cobra.Command {
	var configPath string
	var server string
	var noLogs bool
	var target string

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

			appConfig, format, err := config.LoadAppConfig(configPath)
			if err != nil {
				ui.Error("%v", err)
				return
			}

			deployJobs, err := appConfig.Expand(target)
			if err != nil {
				ui.Error("Failed to process deployment targets: %v", err)
				return
			}

			deploymentID := createDeploymentID()

			var wg sync.WaitGroup

			for _, job := range deployJobs {
				wg.Add(1)
				go deployJob(job, &wg, configPath, deploymentID, format, noLogs, len(deployJobs) > 1)
			}

			wg.Wait()
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to config file or directory")
	cmd.Flags().StringVarP(&server, "server", "s", "", "Haloy server URL (overrides config)")
	cmd.Flags().BoolVar(&noLogs, "no-logs", false, "Don't stream deployment logs")
	cmd.Flags().StringVarP(&target, "target", "t", "", "Deploy to a specific target")

	return cmd
}

func deployJob(job config.DeploymentJob, wg *sync.WaitGroup, configPath, deploymentID, format string, noLogs, showTargetName bool) {
	defer wg.Done()
	prefix := ""
	if showTargetName {
		prefix = lipgloss.NewStyle().Bold(true).Foreground(ui.White).Render(fmt.Sprintf("%s ", job.TargetName))
	}
	pui := &ui.PrefixedUI{Prefix: prefix}

	pui.Info("Deployment started for %s", job.Config.Name)

	if len(job.Config.PreDeploy) > 0 {
		for _, hookCmd := range job.Config.PreDeploy {
			if err := executeHook(hookCmd, getHooksWorkDir(configPath)); err != nil {
				pui.Error("Pre-deploy hook failed: %v", err)
				return
			}
		}
	}

	targetServer, err := getServer(job.Config, "")
	if err != nil {
		pui.Error("%v", err)
		return
	}

	token, err := getToken(job.Config, targetServer)
	if err != nil {
		pui.Error("%v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultContextTimeout)
	defer cancel()

	// Send the deploy request
	api := apiclient.New(targetServer, token)
	request := apitypes.DeployRequest{AppConfig: *job.Config, DeploymentID: deploymentID, ConfigFormat: format}
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

	if len(job.Config.PostDeploy) > 0 {
		for _, hookCmd := range job.Config.PostDeploy {
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
