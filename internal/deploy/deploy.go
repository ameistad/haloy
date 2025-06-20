package deploy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/docker/docker/client"
	"gopkg.in/yaml.v3"
)

func createDeploymentID() string {
	// Generate a unique deployment ID based on the current time.
	// This format is YYYYMMDDHHMMSS, which is sortable and unique.
	return time.Now().Format("20060102150405")

}

func DeployApp(ctx context.Context, cli *client.Client, appConfig *config.AppConfig, imageTag string) error {
	if imageTag == "" {
		return fmt.Errorf("image tag cannot be empty")
	}
	// Ensure that the custom docker network and required services are running.
	if err := docker.EnsureNetwork(cli, ctx); err != nil {
		return fmt.Errorf("failed to ensure Docker network exists: %w", err)
	}
	if _, err := docker.EnsureServicesIsRunning(cli, ctx); err != nil {
		return fmt.Errorf("failed to ensure dependent services are running: %w", err)
	}

	deploymentID := createDeploymentID()

	newImageTag, err := tagImage(ctx, cli, imageTag, appConfig.Name, deploymentID)
	if err != nil {
		return fmt.Errorf("failed to tag image: %w", err)
	}

	ui.Info("Starting deployment for '%s' with deployment ID: %s", appConfig.Name, deploymentID)

	runResult, err := docker.RunContainer(ctx, cli, deploymentID, newImageTag, appConfig)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("failed to run new container: operation timed out after %v (%w)", DefaultDeployTimeout, err)
		} else if errors.Is(err, context.Canceled) {
			if ctx.Err() != nil {
				return fmt.Errorf("failed to run new container: deployment canceled (%w)", ctx.Err())
			}
			return fmt.Errorf("failed to run new container: docker operation canceled (%w)", err)
		}
		return fmt.Errorf("failed to run new container: %w", err)
	}
	if len(runResult) == 0 {
		return fmt.Errorf("failed to run new container: no containers started")
	}

	ui.Info("Started %d container(s) with deployment ID: %s", len(runResult), deploymentID)

	// Write the deployment to history for rollback purposes.
	deploymentsToKeep := config.DefaultDeploymentsToKeep
	if appConfig.DeploymentsToKeep != nil {
		deploymentsToKeep = *appConfig.DeploymentsToKeep
	}
	// Write the app configuration to the history folder.
	if err := writeAppConfigHistory(appConfig, deploymentID, deploymentsToKeep); err != nil {
		ui.Warn("Failed to write app config history: %v", err)
	}

	// Remove all images except the DeploymentsToKeep newest, the ones tagged as latest and in use.
	if err := docker.RemoveImages(ctx, cli, appConfig.Name, deploymentID, deploymentsToKeep); err != nil {
		ui.Error("image-remove: %v", err)
	}

	// This tails the deployment log file that is created by the manager.
	err = tailDeploymentLog(deploymentID)
	if err != nil {
		ui.Error("Failed to tail deployment log: %v", err)
	}

	return nil
}

func tagImage(ctx context.Context, dockerClient *client.Client, srcRef, appName, deploymentID string) (string, error) {
	dstRef := fmt.Sprintf("%s:%s", appName, deploymentID)

	if srcRef == dstRef { // already tagged
		return dstRef, nil
	}

	if err := dockerClient.ImageTag(ctx, srcRef, dstRef); err != nil {
		return dstRef, fmt.Errorf("tag image: %w", err)
	}
	return dstRef, nil
}

func GetImage(ctx context.Context, dockerClient *client.Client, appConfig *config.AppConfig) (string, error) {

	switch true {
	case appConfig.Source.Dockerfile != nil:
		// Source is a Dockerfile. The image name is derived from the app name.
		imageName := appConfig.Name + ":latest" // Convention for locally built images
		// Not using the dockerClient here, but passing it to the BuildImageCLIParams
		buildImageParams := docker.BuildImageParams{
			Context: ctx,
			// DockerClient: dockerClient,
			ImageName: imageName,
			Source:    appConfig.Source.Dockerfile,
			EnvVars:   appConfig.Env,
		}
		if err := docker.BuildImage(buildImageParams); err != nil {
			// Distinguish between timeout and cancellation
			if errors.Is(err, context.DeadlineExceeded) {
				return "", fmt.Errorf("failed to build image: operation timed out after %v (%w)", DefaultDeployTimeout, err)
			} else if errors.Is(err, context.Canceled) {
				// Check if the cancellation came from the parent deployCtx
				if ctx.Err() != nil {
					return "", fmt.Errorf("failed to build image: deployment canceled (%w)", ctx.Err())
				}
				// Otherwise, it might be an internal cancellation within the Docker op
				return "", fmt.Errorf("failed to build image: docker operation canceled (%w)", err)
			}
			return "", fmt.Errorf("failed to build image: %w", err)
		}

		ui.Info("Built image %s successfully", imageName)
		return imageName, nil

	case appConfig.Source.Image != nil:
		imageName, err := docker.EnsureImageUpToDate(ctx, dockerClient, appConfig.Source.Image)
		if err != nil {
			return imageName, err
		}
		ui.Info("Using pre-built image %s for app '%s'", imageName, appConfig.Name)
		return imageName, nil

	default:
		return "", fmt.Errorf("invalid app source configuration: no source type (Dockerfile or Image) defined for app '%s'", appConfig.Name)
	}

}

func tailDeploymentLog(deploymentID string) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID cannot be empty")
	}

	logsPath, err := config.LogsPath()
	if err != nil {
		return fmt.Errorf("failed to get logs path: %w", err)
	}
	logFile := filepath.Join(logsPath, deploymentID+".log")

	// Retry logic for opening the log file
	var file *os.File
	const maxWait = 10 * time.Second
	const retryInterval = 300 * time.Millisecond
	start := time.Now()
	for {
		file, err = os.Open(logFile)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to open log file: %w", err)
		}
		if time.Since(start) > maxWait {
			return fmt.Errorf("log file %s did not appear after %v", logFile, maxWait)
		}
		time.Sleep(retryInterval)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	timeout := 2 * time.Minute
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		lineCh := make(chan string)
		errCh := make(chan error)

		go func() {
			line, err := reader.ReadString('\n')
			if err != nil {
				errCh <- err
			} else {
				lineCh <- line
			}
		}()

		select {
		case line := <-lineCh:
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(timeout)

			if line == "[LOG END]\n" || line == "[LOG END]\r\n" {
				return nil // Stop tailing on log end marker
			}
			// Print the log line using the custom print function
			printLogLine(line)
		case <-errCh:
			time.Sleep(300 * time.Millisecond)
		case <-timer.C:
			return fmt.Errorf("log tail timed out after %v of inactivity", timeout)
		}
	}
}

func printLogLine(line string) {
	switch {
	case len(line) > 7 && line[:7] == "[INFO] ":
		ui.Info("%s", line[7:])
	case len(line) > 10 && line[:10] == "[SUCCESS] ":
		ui.Success("%s", line[10:])
	case len(line) > 7 && line[:7] == "[DEBUG] ":
		ui.Debug("%s", line[7:])
	case len(line) > 7 && line[:7] == "[WARN] ":
		ui.Warn("%s", line[7:])
	case len(line) > 8 && line[:8] == "[ERROR] ":
		ui.Error("%s", line[8:])
	default:
		fmt.Printf("%s", line)
	}
}

// WriteAppConfigHistory writes the given appConfig to the history folder, naming the file <deploymentID>.yml.
func writeAppConfigHistory(appConfig *config.AppConfig, deploymentID string, deploymentsToKeep int) error {
	// Define the history directory inside the config directory.
	historyPath, err := config.HistoryPath()
	if err != nil {
		return fmt.Errorf("failed to get history directory: %w", err)
	}

	// Create the file name based on the deploymentID.
	historyFilePath := filepath.Join(historyPath, fmt.Sprintf("%s.yml", deploymentID))

	// Marshal the appConfig struct to YAML.
	data, err := yaml.Marshal(appConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal app config: %w", err)
	}

	// Write the YAML data to the file.
	if err := os.WriteFile(historyFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config history file '%s': %w", historyFilePath, err)
	}

	// After writing, prune old history files.
	// List all history files ending with .yml in the history directory.
	files, err := os.ReadDir(historyPath)
	if err != nil {
		return fmt.Errorf("failed to read history directory '%s': %w", historyPath, err)
	}

	var historyFiles []os.DirEntry
	for _, file := range files {
		// Only consider files that are not directories and have a .yml extension, and are not the current deployment file.
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".yml") && file.Name() != fmt.Sprintf("%s.yml", deploymentID) {
			historyFiles = append(historyFiles, file)
		}
	}

	// Sort the files descending by filename (deployment id).
	sort.Slice(historyFiles, func(i, j int) bool {
		return historyFiles[i].Name() > historyFiles[j].Name()
	})

	// Delete files beyond the deploymentsToKeep count.
	if len(historyFiles) > deploymentsToKeep {
		for i := deploymentsToKeep; i < len(historyFiles); i++ {
			filePath := filepath.Join(historyPath, historyFiles[i].Name())
			if err := os.Remove(filePath); err != nil {
				ui.Warn("Failed to remove old history file %s: %v", filePath, err)
			} else {
				ui.Info("Removed old history file %s", filePath)
			}
		}
	}

	return nil
}
