package deploy

import (
	"context"
	"fmt"
	"sort"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func RollbackApp(appConfig *config.AppConfig, targetDeploymentID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultDeployTimeout)
	defer cancel()
	dockerClient, err := docker.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}
	defer dockerClient.Close()

	sortedDeployments, err := sortedDeployments(ctx, dockerClient, appConfig.Name)
	if err != nil {
		return err
	}

	if len(sortedDeployments) < 2 {
		return fmt.Errorf("you only have one deployment for app %s, cannot rollback", appConfig.Name)
	}

	currentDeployment := sortedDeployments[0]
	targetDeployment := sortedDeployments[1]

	// If a target deployment ID is provided, find the corresponding deployment
	if targetDeploymentID != "" {
		found := false
		for _, deployment := range sortedDeployments {
			if deployment.ID == targetDeploymentID {
				targetDeployment = deployment
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("target deployment %s not found in current deployments", targetDeploymentID)
		}
	}

	if err := RollbackToDeployment(ctx, dockerClient, currentDeployment, targetDeployment); err != nil {
		return fmt.Errorf("failed to rollback to deployment %s: %w", targetDeployment.ID, err)
	}

	return nil
}

func RollbackToDeployment(ctx context.Context, dockerClient *client.Client, currentDeployment, targetDeployment sortedDeployment) error {
	// Track which containers we've started so we can clean them up on failure
	startedContainers := []string{}

	// First, start all target containers
	for _, targetContainerID := range targetDeployment.containerIDs {
		// Start the target container
		if err := dockerClient.ContainerStart(ctx, targetContainerID, container.StartOptions{}); err != nil {
			// Clean up any containers we've started
			cleanupStartedContainers(ctx, dockerClient, startedContainers)
			return fmt.Errorf("failed to start target container %s: %w", helpers.SafeIDPrefix(targetContainerID), err)
		}

		// Track this container as started
		startedContainers = append(startedContainers, targetContainerID)
	}

	// Now that all containers are started, check their health
	for _, targetContainerID := range startedContainers {
		if err := docker.HealthCheckContainer(ctx, dockerClient, targetContainerID); err != nil {
			// Clean up all started containers
			cleanupStartedContainers(ctx, dockerClient, startedContainers)
			return fmt.Errorf("target container %s is not healthy: %w", helpers.SafeIDPrefix(targetContainerID), err)
		}
	}

	return nil
}

// cleanupStartedContainers stops all containers that were started during the rollback process
func cleanupStartedContainers(ctx context.Context, dockerClient *client.Client, containerIDs []string) {
	for _, id := range containerIDs {
		stopTimeout := 10 // seconds
		err := dockerClient.ContainerStop(ctx, id, container.StopOptions{Timeout: &stopTimeout})
		if err != nil {
			// Just log the error, don't return it as we're already in an error path
			fmt.Printf("Warning: Failed to stop container %s during cleanup: %v\n", helpers.SafeIDPrefix(id), err)
		}
	}
}

type sortedDeployment struct {
	ID           string
	containerIDs []string
}

// getSortedDeployments retrieves and sorts all deployments for an app by timestamp (newest first)
func sortedDeployments(ctx context.Context, dockerClient *client.Client, appName string) ([]sortedDeployment, error) {
	filtersArgs := filters.NewArgs()
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelRole, config.AppLabelRole))
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, appName))

	// Get all containers, including stopped ones
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filtersArgs,
		All:     true, // Include stopped containers for rollback purposes
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return nil, fmt.Errorf("no containers found for app %s", appName)
	}

	// Group containers by deployment ID
	deployments := make(map[string][]string)
	for _, container := range containers {
		deploymentID := container.Labels[config.LabelDeploymentID]
		if deploymentID == "" {
			continue // Skip containers without deployment ID
		}
		deployments[deploymentID] = append(deployments[deploymentID], container.ID)
	}

	if len(deployments) == 0 {
		return nil, fmt.Errorf("no valid deployment information found for app %s", appName)
	}

	result := make([]sortedDeployment, 0, len(deployments))
	for deploymentID, containerIDs := range deployments {
		result = append(result, sortedDeployment{
			ID:           deploymentID,
			containerIDs: containerIDs,
		})
	}

	// Sort deployments by timestamp (newest first)
	sort.Slice(result, func(i, j int) bool {
		// Reliable timestamp sorting (ISO format or Unix timestamp strings)
		return result[i].ID > result[j].ID
	})

	return result, nil
}
