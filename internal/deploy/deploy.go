package deploy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/docker/docker/client"
)

const (
	LogStreamAddress = "localhost:9000" // Address of the manager's log stream server
)

func DeployApp(appConfig *config.AppConfig) error {
	// Create the primary context for the whole deployment + log streaming
	deployCtx, cancelDeploy := context.WithCancel(context.Background())
	// Ensure cancelDeploy is called eventually to stop the streamer and release resources
	defer cancelDeploy()

	// Create a derived context with a timeout for Docker operations
	// This context will be cancelled if deployCtx is cancelled OR if the timeout expires.
	dockerOpCtx, cancelDockerOps := context.WithTimeout(deployCtx, DefaultDeployTimeout)
	// Ensure the timeout context's resources are released
	defer cancelDockerOps()

	dockerClient, err := docker.NewClient(dockerOpCtx) // Use dockerOpCtx
	if err != nil {
		// Check if the error was due to the overall deployment context being cancelled early
		if errors.Is(err, context.Canceled) && deployCtx.Err() != nil {
			return fmt.Errorf("failed to create Docker client: deployment canceled (%w)", deployCtx.Err())
		}
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer dockerClient.Close()

	// Ensure that the custom docker network and required services are running.
	if err := docker.EnsureNetwork(dockerClient, dockerOpCtx); err != nil {
		return fmt.Errorf("failed to ensure Docker network exists: %w", err)
	}
	if _, err := docker.EnsureServicesIsRunning(dockerClient, dockerOpCtx); err != nil {
		return fmt.Errorf("failed to ensure dependent services are running: %w", err)
	}

	imageName, err := GetImage(dockerOpCtx, dockerClient, appConfig)
	if err != nil {
		return err
	}

	deploymentID := time.Now().Format("20060102150405")
	ui.Info("Starting deployment for app '%s' with deployment ID: %s", appConfig.Name, deploymentID)

	runResult, err := docker.RunContainer(dockerOpCtx, dockerClient, deploymentID, imageName, appConfig)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("failed to run new container: operation timed out after %v (%w)", DefaultDeployTimeout, err)
		} else if errors.Is(err, context.Canceled) {
			if deployCtx.Err() != nil {
				return fmt.Errorf("failed to run new container: deployment canceled (%w)", deployCtx.Err())
			}
			return fmt.Errorf("failed to run new container: docker operation canceled (%w)", err)
		}
		return fmt.Errorf("failed to run new container: %w", err)
	}
	if len(runResult) == 0 {
		return fmt.Errorf("failed to run new container: no containers started")
	}

	ui.Info("Started %d container(s) with deployment ID: %s", len(runResult), deploymentID)

	// Explicitly cancel the primary context *before* waiting.
	// This signals the log streamer to stop.
	cancelDeploy()

	tailDeploymentLog(deploymentID)

	return nil
}

func GetImage(ctx context.Context, dockerClient *client.Client, appConfig *config.AppConfig) (string, error) {

	switch true {
	case appConfig.Source.Dockerfile != nil:
		// Source is a Dockerfile. The image name is derived from the app name.
		imageName := appConfig.Name + ":latest" // Convention for locally built images

		ui.Info("Source is Dockerfile, building image '%s'...", imageName)
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

		return imageName, nil

	case appConfig.Source.Image != nil:
		imageName, err := docker.EnsureImageUpToDate(ctx, dockerClient, appConfig.Source.Image)
		if err != nil {
			return imageName, err
		}
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
