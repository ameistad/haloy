package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type ContainerRunResult struct {
	ID           string
	DeploymentID string
	ReplicaID    int
}

func RunContainer(ctx context.Context, dockerClient *client.Client, deploymentID, imageName string, appConfig *config.AppConfig) ([]ContainerRunResult, error) {

	result := make([]ContainerRunResult, 0, *appConfig.Replicas)

	// Convert AppConfig to ContainerLabels
	cl := config.ContainerLabels{
		AppName:             appConfig.Name,
		DeploymentID:        deploymentID,
		ACMEEmail:           appConfig.ACMEEmail,
		Port:                appConfig.Port,
		HealthCheckPath:     appConfig.HealthCheckPath,
		Domains:             appConfig.Domains,
		Role:                config.AppLabelRole,
		MaxContainersToKeep: strconv.Itoa(*appConfig.MaxContainersToKeep),
	}
	labels := cl.ToLabels()

	// Process environment variables
	var envVars []string
	decryptedEnvVars, err := config.DecryptEnvVars(appConfig.Env)
	if err != nil {
		return result, fmt.Errorf("failed to decrypt environment variables: %w", err)
	}
	for _, v := range decryptedEnvVars {
		value, err := v.GetValue()
		if err != nil {
			return result, fmt.Errorf("failed to get value for env var '%s': %w", v.Name, err)
		}
		envVars = append(envVars, fmt.Sprintf("%s=%s", v.Name, value))
	}

	networkMode := container.NetworkMode(config.DockerNetwork)
	if appConfig.NetworkMode != "" {
		networkMode = container.NetworkMode(appConfig.NetworkMode)
	}
	// Attach to the default docker network if not specified. If not using default network HAProxy will not work.
	// Prepare host configuration - set restart policy and volumes to mount.
	hostConfig := &container.HostConfig{
		NetworkMode:   networkMode,
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Binds:         appConfig.Volumes,
	}

	for i := range make([]struct{}, *appConfig.Replicas) {
		envVars := append(envVars, fmt.Sprintf("HALOY_REPLICA_ID=%d", i+1))
		containerConfig := &container.Config{
			Image:  imageName,
			Labels: labels,
			Env:    envVars,
		}
		containerName := fmt.Sprintf("%s-haloy-%s-replica-%d", appConfig.Name, deploymentID, i+1)
		resp, err := dockerClient.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
		if err != nil {
			return result, fmt.Errorf("failed to create container: %w", err)
		}

		// Ensure the container is removed on error
		// This is important to avoid leaving dangling containers in case of failure.
		// We use a deferred function to ensure cleanup happens even if the function exits early.
		defer func() {
			if err != nil && resp.ID != "" {
				// Try to remove container on error
				removeErr := dockerClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
				if removeErr != nil {
					fmt.Printf("Failed to clean up container after error: %v\n", removeErr)
				}
			}
		}()

		if err := dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
			return result, fmt.Errorf("failed to start container: %w", err)
		}

		result = append(result, ContainerRunResult{
			ID:           resp.ID,
			DeploymentID: deploymentID,
			ReplicaID:    i + 1,
		})

	}

	return result, nil
}

func StopContainers(ctx context.Context, dockerClient *client.Client, appName, ignoreDeploymentID string) ([]string, error) {
	stoppedIDs := []string{}
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, appName))

	containerList, err := dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filterArgs,
		All:     false, // Only running containers
	})

	for _, containerInfo := range containerList {
		deploymentID := containerInfo.Labels[config.LabelDeploymentID]
		if deploymentID == ignoreDeploymentID {
			continue
		}

		timeout := 20
		stopOptions := container.StopOptions{
			Timeout: &timeout,
		}
		err := dockerClient.ContainerStop(ctx, containerInfo.ID, stopOptions)
		if err != nil {
			ui.Warn("Error stopping container %s: %v\n", helpers.SafeIDPrefix(containerInfo.ID), err)
		} else {
			stoppedIDs = append(stoppedIDs, containerInfo.ID)
		}
	}
	if err != nil {
		return stoppedIDs, fmt.Errorf("failed to list containers: %w", err)
	}
	return stoppedIDs, nil
}

type RemoveContainersParams struct {
	Context             context.Context
	DockerClient        *client.Client
	AppName             string
	IgnoreDeploymentID  string
	MaxContainersToKeep int
}

type RemoveContainersResult struct {
	ID           string
	DeploymentID string
}

// RemoveContainers attempts to remove old containers for a given app,
// respecting a retention policy (MaxContainersToKeep) and ignoring a specific deployment.
// It returns a slice of RemoveContainersResult for successfully removed containers
// and an error if the listing fails or if any of the removal operations fail.
func RemoveContainers(params RemoveContainersParams) (successfullyRemoved []RemoveContainersResult, err error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, params.AppName))

	containerList, listErr := params.DockerClient.ContainerList(params.Context, container.ListOptions{
		Filters: filterArgs,
		All:     true,
	})
	if listErr != nil {
		return nil, fmt.Errorf("failed to list containers: %w", listErr)
	}

	var candidates []RemoveContainersResult
	for _, c := range containerList {
		deploymentID := c.Labels[config.LabelDeploymentID]
		if deploymentID == params.IgnoreDeploymentID {
			continue
		}
		candidates = append(candidates, RemoveContainersResult{
			ID:           c.ID,
			DeploymentID: deploymentID,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].DeploymentID > candidates[j].DeploymentID
	})

	var containersToAttemptRemoval []RemoveContainersResult
	if params.MaxContainersToKeep >= 0 && len(candidates) > params.MaxContainersToKeep {
		containersToAttemptRemoval = candidates[params.MaxContainersToKeep:]
	} else if params.MaxContainersToKeep == 0 { // Explicitly handle keep 0 when len(candidates) might also be 0
		containersToAttemptRemoval = candidates
	} else {
		containersToAttemptRemoval = nil // No containers to remove from the candidates list
	}

	var removalErrors []error
	successfullyRemoved = make([]RemoveContainersResult, 0, len(containersToAttemptRemoval))

	for _, c := range containersToAttemptRemoval {
		errRemove := params.DockerClient.ContainerRemove(params.Context, c.ID, container.RemoveOptions{Force: true})
		if errRemove != nil {
			ui.Warn("Error removing container %s (DeploymentID: %s): %v\n", helpers.SafeIDPrefix(c.ID), c.DeploymentID, errRemove)
			removalErrors = append(removalErrors, fmt.Errorf("failed to remove container %s: %w", c.ID, errRemove))
		} else {
			successfullyRemoved = append(successfullyRemoved, c)
		}
	}

	if len(removalErrors) > 0 {
		return successfullyRemoved, errors.Join(removalErrors...)
	}

	return successfullyRemoved, nil
}

func HealthCheckContainer(ctx context.Context, dockerClient *client.Client, logger *logging.Logger, containerID string, initialWaitTime ...time.Duration) error {
	// Check if container is running - wait up to 30 seconds for it to start
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var containerInfo container.InspectResponse
	var err error

	// Wait for container to be running
	for {
		containerInfo, err = dockerClient.ContainerInspect(startCtx, containerID)
		if err != nil {
			return fmt.Errorf("failed to inspect container %s: %w", helpers.SafeIDPrefix(containerID), err)
		}

		if containerInfo.State.Running {
			break
		}

		select {
		case <-startCtx.Done():
			return fmt.Errorf("timed out waiting for container %s to start", helpers.SafeIDPrefix(containerID))
		case <-time.After(500 * time.Millisecond):
		}
	}

	if len(initialWaitTime) > 0 && initialWaitTime[0] > 0 {
		waitTime := initialWaitTime[0]

		waitTimer := time.NewTimer(waitTime)
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled during initial wait period")
		case <-waitTimer.C:
		}
	}

	// Check if container has built-in Docker healthcheck
	if containerInfo.State.Health != nil {

		// If container has healthcheck and it's healthy, we can skip our manual check
		if containerInfo.State.Health.Status == "healthy" {
			return nil
		}

		// Wait for Docker healthcheck to transition from starting state
		if containerInfo.State.Health.Status == "starting" {
			healthCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			for {
				containerInfo, err = dockerClient.ContainerInspect(healthCtx, containerID)
				if err != nil {
					return fmt.Errorf("failed to re-inspect container: %w", err)
				}

				if containerInfo.State.Health.Status != "starting" {
					break
				}

				select {
				case <-healthCtx.Done():
					return fmt.Errorf("timed out waiting for container health check to complete")
				case <-time.After(1 * time.Second):
					// Continue polling
				}
			}
		}

		// If container has healthcheck and it's healthy, we can skip our manual check
		if containerInfo.State.Health.Status == "healthy" {
			logger.Debug(fmt.Sprintf("Container %s is healthy according to Docker healthcheck", helpers.SafeIDPrefix(containerID)))
			return nil
		} else if containerInfo.State.Health.Status == "unhealthy" {
			// Log health check failure details if available
			if len(containerInfo.State.Health.Log) > 0 {
				lastLog := containerInfo.State.Health.Log[len(containerInfo.State.Health.Log)-1]
				return fmt.Errorf("container %s is unhealthy: %s", helpers.SafeIDPrefix(containerID), lastLog.Output)
			}
			return fmt.Errorf("container %s is unhealthy according to Docker healthcheck", helpers.SafeIDPrefix(containerID))
		}
	}

	// Rest of the existing HTTP health check code remains the same...
	labels, err := config.ParseContainerLabels(containerInfo.Config.Labels)
	if err != nil {
		return fmt.Errorf("failed to parse container labels: %w", err)
	}

	if labels.Port == "" {
		return fmt.Errorf("container %s has no port label set", helpers.SafeIDPrefix(containerID))
	}

	if labels.HealthCheckPath == "" {
		return fmt.Errorf("container %s has no health check path set", helpers.SafeIDPrefix(containerID))
	}

	targetIP, err := ContainerNetworkIP(containerInfo, config.DockerNetwork)
	if err != nil {
		return fmt.Errorf("failed to get container IP address: %w", err)
	}

	// Construct URL for health check
	healthCheckURL := fmt.Sprintf("http://%s:%s%s", targetIP, labels.Port, labels.HealthCheckPath)

	// Perform health check with retries
	maxRetries := 5
	backoff := 500 * time.Millisecond

	httpClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Use traditional for loop for clarity
	for retry := 0; retry < maxRetries; retry++ {
		if retry > 0 {
			logger.Info(fmt.Sprintf("Retrying health check in %v... (attempt %d/%d)\n", backoff, retry+1, maxRetries))
			time.Sleep(backoff)
			backoff *= 2 // Exponential backoff
		}

		req, err := http.NewRequestWithContext(ctx, "GET", healthCheckURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create health check request: %w", err)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			logger.Warn("Health check attempt failed", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		logger.Warn(fmt.Sprintf("Health check returned status %d: %s", resp.StatusCode, string(bodyBytes)))
	}

	return fmt.Errorf("container %s failed health check after %d attempts", helpers.SafeIDPrefix(containerID), maxRetries)
}

// PruneContainers removes stopped containers and returns the number of containers deleted and bytes reclaimed.
func PruneContainers(ctx context.Context, dockerClient *client.Client) (int, uint64, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelRole, config.AppLabelRole))
	report, err := dockerClient.ContainersPrune(ctx, filterArgs)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to prune containers: %w", err)
	}
	if len(report.ContainersDeleted) > 0 {
		ui.Info("Pruned %d containers, reclaimed %d bytes", len(report.ContainersDeleted), report.SpaceReclaimed)
	}
	return len(report.ContainersDeleted), report.SpaceReclaimed, nil
}
