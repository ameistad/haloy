package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
)

const (
	LogStreamAddress = "localhost:9000" // Address of the manager's log stream server
)

func DeployApp(appConfig *config.AppConfig) error {
	// 1. Create the primary context for the whole deployment + log streaming
	deployCtx, cancelDeploy := context.WithCancel(context.Background())
	// Ensure cancelDeploy is called eventually to stop the streamer and release resources
	defer cancelDeploy()

	// Use a WaitGroup to wait for the log streamer goroutine to finish
	var wg sync.WaitGroup

	// Start log streaming in a separate goroutine, passing the primary context
	wg.Add(1)
	go streamLogs(deployCtx, &wg, appConfig.Name)

	// 2. Create a derived context with a timeout for Docker operations
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

	imageName := appConfig.Name + ":latest"

	ui.Info("Building image '%s'...\n", imageName)
	buildImageParams := docker.BuildImageParams{
		Context:      dockerOpCtx, // Use dockerOpCtx
		DockerClient: dockerClient,
		ImageName:    imageName,
		Source:       appConfig.Source.Dockerfile,
		EnvVars:      appConfig.Env,
	}
	if err := docker.BuildImage(buildImageParams); err != nil {
		// Distinguish between timeout and cancellation
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("failed to build image: operation timed out after %v (%w)", DefaultDeployTimeout, err)
		} else if errors.Is(err, context.Canceled) {
			// Check if the cancellation came from the parent deployCtx
			if deployCtx.Err() != nil {
				return fmt.Errorf("failed to build image: deployment canceled (%w)", deployCtx.Err())
			}
			// Otherwise, it might be an internal cancellation within the Docker op
			return fmt.Errorf("failed to build image: docker operation canceled (%w)", err)
		}
		return fmt.Errorf("failed to build image: %w", err)
	}
	ui.Success("Image '%s' built successfully.\n", imageName)

	ui.Info("Running new container(s) for '%s'...\n", appConfig.Name)
	runResult, err := docker.RunContainer(dockerOpCtx, dockerClient, imageName, appConfig) // Use dockerOpCtx
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
	for _, container := range runResult {
		ui.Info("New container '%s' started successfully.\n", helpers.SafeIDPrefix(container.ID))
	}

	ui.Info("Stopping old container(s) for '%s' (excluding deployment %s)...\n", appConfig.Name, deploymentID)
	// Use dockerOpCtx for stopping containers
	if err := docker.StopContainers(dockerOpCtx, dockerClient, appConfig.Name, deploymentID); err != nil {
		// Log warning but don't necessarily fail the whole deployment
		ui.Warning("Failed to stop old containers: %v\n", err)
	} else {
		ui.Info("Old container(s) stopped.\n")
	}

	ui.Info("Removing old container(s) for '%s' (excluding deployment %s)...\n", appConfig.Name, deploymentID)
	removeContainersParams := docker.RemoveContainersParams{
		Context:             dockerOpCtx, // Use dockerOpCtx
		DockerClient:        dockerClient,
		AppName:             appConfig.Name,
		IgnoreDeploymentID:  deploymentID,
		MaxContainersToKeep: *appConfig.MaxContainersToKeep,
	}
	removedContainers, err := docker.RemoveContainers(removeContainersParams)
	if err != nil {
		ui.Warning("Failed to remove old containers: %v\n", err)
	}

	if len(removedContainers) == 0 {
		ui.Info("No old containers to remove.\n")
	} else {
		suffix := ""
		if len(removedContainers) > 1 {
			suffix = "s"
		}
		ui.Info("Removed %d old container%s\n", len(removedContainers), suffix)
	}

	// --- Deployment Finished Successfully ---

	// 3. Explicitly cancel the primary context *before* waiting.
	// This signals the log streamer to stop.
	cancelDeploy()

	// 4. Wait for the log streamer goroutine to finish cleanly.
	wg.Wait()

	ui.Success("Successfully deployed app '%s'. New deployment ID: %s\n", appConfig.Name, deploymentID)
	return nil // Success
}

func streamLogs(ctx context.Context, wg *sync.WaitGroup, appName string) {
	defer wg.Done()

	clientConfig := logging.ClientConfig{
		Address:       LogStreamAddress,
		AppNameFilter: appName,
		UseDeadline:   true, // Use deadlines for deploy command
		// ReadDeadline defaults to 500ms in NewLogStreamClient
		// DialTimeout defaults to 5s in NewLogStreamClient
	}

	ui.Info("Attempting to connect to log stream at %s for app '%s'...\n", clientConfig.Address, appName)
	client, err := logging.NewLogStreamClient(clientConfig)
	if err != nil {
		ui.Warning("Could not connect to log stream: %v. Continuing deployment without live logs.\n", err)
		return
	}
	defer client.Close()
	ui.Success("Connected to log stream. Filtering for '%s'.\n", appName)

	// Stream logs to stdout
	// Prefix output to distinguish stream logs
	prefixWriter := helpers.NewPrefixWriter(os.Stdout, "[STREAM] ")
	err = client.Stream(ctx, prefixWriter)

	// Handle stream exit reason
	if err != nil && !errors.Is(err, context.Canceled) {
		// Log errors other than context cancellation (which is expected on deploy finish)
		ui.Error("Log stream error: %v\n", err)
	} else {
		ui.Info("Log stream finished.\n")
	}
}
