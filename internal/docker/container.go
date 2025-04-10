package docker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

type ContainerRunResult struct {
	ID           string
	DeploymentID string
	ReplicaID    int
}

func RunContainer(ctx context.Context, dockerClient *client.Client, imageName string, appConfig *config.AppConfig) ([]ContainerRunResult, error) {
	deploymentID := time.Now().Format("20060102150405")
	result := make([]ContainerRunResult, 0, *appConfig.Replicas)

	// Convert AppConfig to ContainerLabels
	cl := config.ContainerLabels{
		AppName:         appConfig.Name,
		DeploymentID:    deploymentID,
		Ignore:          false,
		ACMEEmail:       appConfig.ACMEEmail,
		Port:            appConfig.Port,
		HealthCheckPath: appConfig.HealthCheckPath,
		Domains:         appConfig.Domains,
		Role:            config.AppLabelRole,
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

	// Prepare host configuration - set restart policy and volumes to mount.
	hostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Binds:         appConfig.Volumes,
	}

	// Ensure that the custom network and required services are running.
	if err := EnsureNetwork(dockerClient, ctx); err != nil {
		return result, fmt.Errorf("failed to ensure Docker network exists: %w", err)
	}
	if _, err := EnsureServicesIsRunning(dockerClient, ctx); err != nil {
		return result, fmt.Errorf("failed to ensure dependent services are running: %w", err)
	}

	// Attach the container to the predefined network
	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			config.DockerNetwork: {},
		},
	}

	for i := range make([]struct{}, *appConfig.Replicas) {
		envVars := append(envVars, fmt.Sprintf("HALOY_REPLICA_ID=%d", i+1))
		containerConfig := &container.Config{
			Image:  imageName,
			Labels: labels,
			Env:    envVars,
		}
		containerName := fmt.Sprintf("%s-haloy-%s-replica-%d", appConfig.Name, deploymentID, i+1)
		resp, err := dockerClient.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, nil, containerName)
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

func StopContainers(ctx context.Context, dockerClient *client.Client, appName, ignoreDeploymentID string) error {
	filter := filters.NewArgs()
	filter.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, appName))

	containerList, err := dockerClient.ContainerList(ctx, container.ListOptions{
		Filters: filter,
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
			ui.Warning("Error stopping container %s: %v\n", helpers.SafeIDPrefix(containerInfo.ID), err)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}
	return nil
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

func RemoveContainers(params RemoveContainersParams) ([]RemoveContainersResult, error) {
	filter := filters.NewArgs()
	filter.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, params.AppName))

	containerList, err := params.DockerClient.ContainerList(params.Context, container.ListOptions{
		Filters: filter,
		All:     true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var containers []RemoveContainersResult

	// Filter out the container with IgnoreDeploymentID
	for _, c := range containerList {
		deploymentID := c.Labels[config.LabelDeploymentID]
		if deploymentID == params.IgnoreDeploymentID {
			continue
		}
		containers = append(containers, RemoveContainersResult{
			ID:           c.ID,
			DeploymentID: deploymentID,
		})
	}

	// Sort containers by deploymentID (newest/largest timestamp first)
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].DeploymentID > containers[j].DeploymentID
	})

	// Skip newest containers according to NumberOfContainersToSkip
	removedContainers := []RemoveContainersResult{}
	if params.MaxContainersToKeep == 0 {
		// Remove all containers except the one with IgnoreDeploymentID
		removedContainers = containers
	} else if params.MaxContainersToKeep > 0 && len(containers) > params.MaxContainersToKeep {
		containersToKeep := containers[:params.MaxContainersToKeep]
		removedContainers = containers[params.MaxContainersToKeep:]

		_ = containersToKeep // just to avoid linter error
	}

	// Remove the remaining containers
	for _, c := range removedContainers {
		err := params.DockerClient.ContainerRemove(params.Context, c.ID, container.RemoveOptions{Force: true})
		if err != nil {
			ui.Warning("Error removing container %s: %v\n", helpers.SafeIDPrefix(c.ID), err)
		}
	}

	return removedContainers, nil
}

func HealthCheckContainer(ctx context.Context, dockerClient *client.Client, containerID string, initialWaitTime ...time.Duration) error {
	// Check if container is running - wait up to 30 seconds for it to start
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var containerInfo container.InspectResponse
	var err error

	// Wait for container to be running
	ui.Info("Waiting for container %s to be running...", helpers.SafeIDPrefix(containerID))
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
		ui.Info("Waiting %v for container to initialize...", waitTime)

		waitTimer := time.NewTimer(waitTime)
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled during initial wait period")
		case <-waitTimer.C:
		}
	}

	// Check if container has built-in Docker healthcheck
	if containerInfo.State.Health != nil {
		ui.Info("Container has built-in health check, status: %s", containerInfo.State.Health.Status)

		// If container has healthcheck and it's healthy, we can skip our manual check
		if containerInfo.State.Health.Status == "healthy" {
			ui.Success("Container %s is healthy according to Docker healthcheck", helpers.SafeIDPrefix(containerID))
			return nil
		}
	}

	// Check if container has built-in Docker healthcheck
	if containerInfo.State.Health != nil {
		ui.Info("Container has built-in health check, status: %s", containerInfo.State.Health.Status)

		// Wait for Docker healthcheck to transition from starting state
		if containerInfo.State.Health.Status == "starting" {
			healthCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			ui.Info("Waiting for built-in health check to complete...")
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
			ui.Success("Container %s is healthy according to Docker healthcheck", helpers.SafeIDPrefix(containerID))
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
			ui.Info("Retrying health check in %v... (attempt %d/%d)\n", backoff, retry+1, maxRetries)
			time.Sleep(backoff)
			backoff *= 2 // Exponential backoff
		}

		req, err := http.NewRequestWithContext(ctx, "GET", healthCheckURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create health check request: %w", err)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			ui.Warning("Health check attempt failed: %v", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			ui.Success("Health check passed for container %s\n", helpers.SafeIDPrefix(containerID))
			return nil
		}

		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		ui.Warning("Health check returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return fmt.Errorf("container %s failed health check after %d attempts", helpers.SafeIDPrefix(containerID), maxRetries)
}
