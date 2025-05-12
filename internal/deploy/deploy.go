package deploy

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/docker/docker/client"
	"github.com/rs/zerolog"
)

const (
	LogStreamAddress = "localhost:9000" // Address of the manager's log stream server
)

func DeployApp(appConfig *config.AppConfig) error {

	// printerDeployStatus, _ := pterm.DefaultSpinner.Start("Starting deployment...")

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
	ui.Info("Network and services are running")

	// Use a WaitGroup to wait for the log streamer goroutine to finish
	var wg sync.WaitGroup

	// Start log streaming in a separate goroutine, passing the primary context
	wg.Add(1)
	go streamLogs(deployCtx, &wg, appConfig.Name)

	imageName, err := GetImage(dockerOpCtx, dockerClient, appConfig)
	if err != nil {
		return err
	}

	runResult, err := docker.RunContainer(dockerOpCtx, dockerClient, imageName, appConfig)
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

	deploymentID := runResult[0].DeploymentID
	ui.Info("Started %d container(s) with deployment ID: %s", len(runResult), deploymentID)

	if err := docker.StopContainers(dockerOpCtx, dockerClient, appConfig.Name, deploymentID); err != nil {
		ui.Warn("Failed to stop old containers: %v\n", err)
	}
	removeContainersParams := docker.RemoveContainersParams{
		Context:             dockerOpCtx, // Use dockerOpCtx
		DockerClient:        dockerClient,
		AppName:             appConfig.Name,
		IgnoreDeploymentID:  deploymentID,
		MaxContainersToKeep: *appConfig.MaxContainersToKeep,
	}
	removedContainers, err := docker.RemoveContainers(removeContainersParams)
	if err != nil {
		ui.Warn("Failed to remove old containers: %v\n", err)
	}
	ui.Info("Cleanup complete:\nStopped %d container(s)\nRemoved %d old container(s)", len(runResult), len(removedContainers))

	// Explicitly cancel the primary context *before* waiting.
	// This signals the log streamer to stop.
	cancelDeploy()

	// Wait for the log streamer goroutine to finish cleanly.
	wg.Wait()

	ui.Success("Successfully deployed %s", appConfig.Name)

	return nil
}

func streamLogs(ctx context.Context, wg *sync.WaitGroup, appName string) {
	defer wg.Done()

	clientConfig := logging.ClientConfig{
		AppNameFilter: appName,
		UseDeadline:   true,
		MinLevel:      zerolog.InfoLevel,
		Handler:       sharedLogHandler(),
	}

	client, err := logging.NewLogStreamClient(clientConfig)
	if err != nil {
		ui.Warn("Could not connect to log stream from manager: %v. Continuing deployment without live logs.", err)
		return
	}
	defer client.Close()

	err = client.Stream(ctx) // Pass os.Stdout directly

	// Handle stream exit reason
	if err != nil && !errors.Is(err, context.Canceled) {
		ui.Error("Log stream error: %v\n", err)
	}
}

func GetImage(ctx context.Context, dockerClient *client.Client, appConfig *config.AppConfig) (string, error) {

	switch true {
	case appConfig.Source.Dockerfile != nil:
		// Source is a Dockerfile. The image name is derived from the app name.
		imageName := appConfig.Name + ":latest" // Convention for locally built images

		ui.Info("Source is Dockerfile, building image '%s'...", imageName)
		// Not using the dockerClient here, but passing it to the BuildImageCLIParams
		buildImageParams := docker.BuildImageCLIParams{
			Context: ctx,
			// DockerClient: dockerClient,
			ImageName:  imageName,
			Source:     appConfig.Source.Dockerfile,
			EnvVars:    appConfig.Env,
			LogHandler: sharedLogHandler(),
		}
		if err := docker.BuildImageCLI(buildImageParams); err != nil {
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
		// Source is a pre-existing image.
		imgSource := appConfig.Source.Image
		imageName := imgSource.Repository
		tag := imgSource.Tag
		if tag == "" {
			tag = "latest" // Default to latest tag if not specified
		}
		imageName = imageName + ":" + tag
		return imageName, nil

	default:
		return "", fmt.Errorf("invalid app source configuration: no source type (Dockerfile or Image) defined for app '%s'", appConfig.Name)
	}

}

func sharedLogHandler() logging.LogHandlerFunc {
	return func(level zerolog.Level, message string, appName string) {
		switch level {
		case zerolog.DebugLevel:
		case zerolog.InfoLevel:
			ui.Info("%s", message)
		case zerolog.WarnLevel:
			ui.Warn(message, "")
		case zerolog.ErrorLevel:
		case zerolog.FatalLevel:
		case zerolog.PanicLevel:
			ui.Error("%s", message)
		default:
			ui.Info("%s", message)
		}
	}
}
