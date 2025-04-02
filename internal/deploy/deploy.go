package deploy

import (
	"context"
	"errors"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/ui"
)

const (
	deployTimeout = 5 * time.Minute
)

func DeployApp(appConfig *config.AppConfig) {

	ctx, cancel := context.WithTimeout(context.Background(), deployTimeout)
	defer cancel()
	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		ui.Error("%v", err)
		return
	}
	defer dockerClient.Close()

	imageName := appConfig.Name + ":latest"

	if err := docker.BuildImage(ctx, dockerClient, imageName, appConfig.Source.Dockerfile); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			ui.Error("Failed to build image: operation timed out (%v)", err)
		} else if errors.Is(err, context.Canceled) {
			ui.Error("Failed to build image: operation canceled (%v)", err)
		} else {
			ui.Error("Failed to build image: %v", err)
		}
		return
	}
	ui.Info("Image '%s' built successfully.", imageName)

	// containerID, deploymentID, err := runContainer(ctx, dockerClient, imageName, appConfig)
	runResult, err := docker.RunContainer(ctx, dockerClient, imageName, appConfig)
	if err != nil {
		// Check for context errors
		if errors.Is(err, context.DeadlineExceeded) {
			ui.Error("Failed to run new container: operation timed out (%v)", err)
		} else if errors.Is(err, context.Canceled) {
			ui.Error("Failed to run new container: operation canceled (%v)", err)
		} else {
			ui.Error("Failed to run new container: %v", err)
		}
		return
	}

	ui.Info("New container '%s' started successfully.", runResult.ContainerID[:12])

	ui.Info("Stopping old containers...")
	if err := docker.StopContainers(ctx, dockerClient, appConfig.Name, runResult.DeploymentID); err != nil {
		ui.Error("Failed to stop old containers: %v", err)
		return
	}

	ui.Info("Keeping %d old containers", appConfig.MaxContainersToKeep)
	if err := docker.RemoveContainers(docker.RemoveContainersParams{
		Context:             ctx,
		DockerClient:        dockerClient,
		AppName:             appConfig.Name,
		IgnoreDeploymentID:  runResult.DeploymentID,
		MaxContainersToKeep: appConfig.MaxContainersToKeep,
	}); err != nil {
		ui.Error("Failed to remove old containers: %v", err)
		return
	}
	ui.Info("Old containers stopped and removed successfully.")

	// // Prune old containers based on configuration.
	// if err := PruneOldContainers(appConfig.Name, runResult.ContainerID, appConfig.MaxContainersToKeep); err != nil {
	// 	ui.Error("Failed to prune old containers: %w", err)
	// 	return
	// }

	// // Clean up old dangling images
	// if err := PruneOldImages(appConfig.Name); err != nil {
	// 	ui.Warning("Warning: failed to prune old images: %v\n", err)
	// 	// We don't return the error here as this is a non-critical step
	// }

	ui.Success("Successfully deployed app '%s'. New deployment ID: %s\n", appConfig.Name, runResult.DeploymentID)
}
