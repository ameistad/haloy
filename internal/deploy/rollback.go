package deploy

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
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

	deployments, err := deployments(ctx, dockerClient, appConfig.Name)
	if err != nil {
		return err
	}

	if len(deployments.oldDeployments) == 0 {
		return fmt.Errorf("there are no older deployments to rollback to for %s", appConfig.Name)
	}

	targetDeployment := deployments.oldDeployments[0]

	// If a target deployment ID is provided, find the corresponding deployment
	if targetDeploymentID != "" {
		found := false
		for _, deployment := range deployments.oldDeployments {
			if deployment.ID == targetDeploymentID {
				targetDeployment = deployment
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("target deployment %s not found in previous deployments", targetDeploymentID)
		}
	}

	if targetDeployment.ID == deployments.currentDeployment.ID {
		return fmt.Errorf("target deployment %s is already the current deployment", targetDeployment.ID)
	}

	if err := RollbackToDeployment(ctx, dockerClient, appConfig.Name, deployments.currentDeployment, targetDeployment); err != nil {
		return fmt.Errorf("failed to rollback to deployment %s: %w", targetDeployment.ID, err)
	}

	return nil
}

func RollbackToDeployment(ctx context.Context, dockerClient *client.Client, appName string, currentDeployment, targetDeployment deploymentInfo) error {
	// Track which containers we've started so we can clean them up on failure
	startError := false
	healthCheckError := false

	for _, targetContainerID := range targetDeployment.containerIDs {
		ui.Info("Starting target container %s...", helpers.SafeIDPrefix(targetContainerID))
		if err := dockerClient.ContainerStart(ctx, targetContainerID, container.StartOptions{}); err != nil {
			startError = true
			ui.Error("failed to start target container %s: %v", helpers.SafeIDPrefix(targetContainerID), err)
			break
		}
	}

	if !startError {
		for _, targetContainerID := range targetDeployment.containerIDs {
			ui.Info("Checking health of target container %s...", helpers.SafeIDPrefix(targetContainerID))
			// Give the container some time to start
			healtCheckDelay := 3 * time.Second
			if err := docker.HealthCheckContainer(ctx, dockerClient, targetContainerID, healtCheckDelay); err != nil {
				healthCheckError = true
				ui.Error("target container %s is not healthy: %v", helpers.SafeIDPrefix(targetContainerID), err)
				break
			}
		}

	}

	ignoreDeploymentID := targetDeployment.ID
	if startError || healthCheckError {
		ui.Error("Rollback failed, cleaning up started containers...")
		ignoreDeploymentID = currentDeployment.ID
	}
	if err := docker.StopContainers(ctx, dockerClient, appName, ignoreDeploymentID); err != nil {
		return fmt.Errorf("failed to stop containers for app %s: %w", appName, err)
	}

	if startError {
		return fmt.Errorf("failed to start target containers for app %s", appName)
	}
	if healthCheckError {
		return fmt.Errorf("target containers for app %s are not healthy", appName)
	}

	return nil
}

type deploymentInfo struct {
	ID           string
	containerIDs []string
}

type deploymentsResult struct {
	currentDeployment deploymentInfo
	oldDeployments    []deploymentInfo
}

// deployments retrieves and sorts all deployments for an app by timestamp (newest first)
func deployments(ctx context.Context, dockerClient *client.Client, appName string) (deploymentsResult, error) {

	result := deploymentsResult{}

	filtersArgs := filters.NewArgs()
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelRole, config.AppLabelRole))
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, appName))

	// Get all containers, including stopped ones
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filtersArgs,
		All:     true, // Include stopped containers for rollback purposes
	})
	if err != nil {
		return result, fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return result, fmt.Errorf("no containers found for app %s", appName)
	}

	runningDeployment := deploymentInfo{
		ID:           "",
		containerIDs: []string{},
	}
	deployments := make(map[string][]string)
	for _, container := range containers {
		deploymentID := container.Labels[config.LabelDeploymentID]
		if deploymentID == "" {
			continue
		}

		// Add ALL containers to deployments map regardless of state
		deployments[deploymentID] = append(deployments[deploymentID], container.ID)

		// Only update running deployment for running containers
		if container.State == "running" {
			runningDeployment = deploymentInfo{
				ID:           deploymentID,
				containerIDs: append(runningDeployment.containerIDs, container.ID),
			}
		}
	}

	if len(deployments) == 0 {
		return result, fmt.Errorf("no valid deployment information found for app %s", appName)
	}

	oldDeployments := make([]deploymentInfo, 0, len(deployments))
	for deploymentID, containerIDs := range deployments {
		if deploymentID == runningDeployment.ID {
			continue
		}

		// Skip deployment that are newer than the running deployment.
		if deploymentID > runningDeployment.ID {
			continue
		}
		oldDeployments = append(oldDeployments, deploymentInfo{
			ID:           deploymentID,
			containerIDs: containerIDs,
		})
	}

	// Sort deployments by timestamp (newest first)
	sort.Slice(oldDeployments, func(i, j int) bool {
		// Reliable timestamp sorting (ISO format or Unix timestamp strings)
		return oldDeployments[i].ID > oldDeployments[j].ID
	})

	result = deploymentsResult{
		currentDeployment: runningDeployment,
		oldDeployments:    oldDeployments,
	}
	return result, nil
}
