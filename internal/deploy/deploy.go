package deploy

import (
	"context"
	"errors"
	"fmt"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
)

func DeployApp(appConfig *config.AppConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultDeployTimeout)
	defer cancel()
	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)

	}
	defer dockerClient.Close()

	imageName := appConfig.Name + ":latest"

	buildImageParams := docker.BuildImageParams{
		Context:      ctx,
		DockerClient: dockerClient,
		ImageName:    imageName,
		Source:       appConfig.Source.Dockerfile,
		EnvVars:      appConfig.Env,
	}
	if err := docker.BuildImage(buildImageParams); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("failed to build image: operation timed out (%w)", err)
		} else if errors.Is(err, context.Canceled) {
			return fmt.Errorf("failed to build image: operation canceled (%w)", err)
		}
		return fmt.Errorf("failed to build image: %w", err)
	}

	// containerID, deploymentID, err := runContainer(ctx, dockerClient, imageName, appConfig)
	runResult, err := docker.RunContainer(ctx, dockerClient, imageName, appConfig)
	if err != nil {
		// Check for context errors
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("failed to run new container: operation timed out (%w)", err)
		} else if errors.Is(err, context.Canceled) {
			return fmt.Errorf("failed to run new container: operation canceled (%w)", err)
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

	if err := docker.StopContainers(ctx, dockerClient, appConfig.Name, deploymentID); err != nil {
		return fmt.Errorf("failed to stop old containers: %w", err)
	}

	removeContainersParams := docker.RemoveContainersParams{
		Context:             ctx,
		DockerClient:        dockerClient,
		AppName:             appConfig.Name,
		IgnoreDeploymentID:  deploymentID,
		MaxContainersToKeep: *appConfig.MaxContainersToKeep,
	}
	removedContainers, err := docker.RemoveContainers(removeContainersParams)
	if err != nil {
		return fmt.Errorf("failed to remove old containers: %w", err)
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

	ui.Success("Successfully deployed app '%s'. New deployment ID: %s\n", appConfig.Name, deploymentID)
	return nil
}
