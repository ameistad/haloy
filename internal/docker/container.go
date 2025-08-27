package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/secrets"
	"github.com/ameistad/haloy/internal/storage"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

type ContainerRunResult struct {
	ID           string
	DeploymentID string
	ReplicaID    int
}

func RunContainer(ctx context.Context, cli *client.Client, deploymentID, imageRef string, appConfig config.AppConfig) ([]ContainerRunResult, error) {

	result := make([]ContainerRunResult, 0, *appConfig.Replicas)

	// Check image platform compatibility before creating containers
	if err := checkImagePlatformCompatibility(ctx, cli, imageRef); err != nil {
		return result, err
	}

	// Convert AppConfig to ContainerLabels
	cl := config.ContainerLabels{
		AppName:         appConfig.Name,
		DeploymentID:    deploymentID,
		ACMEEmail:       appConfig.ACMEEmail,
		Port:            appConfig.Port,
		HealthCheckPath: appConfig.HealthCheckPath,
		Domains:         appConfig.Domains,
		Role:            config.AppLabelRole,
	}
	labels := cl.ToLabels()

	// Process environment variables
	var envVars []string
	var secretEnvVars []config.EnvVar

	for _, envVar := range appConfig.Env {
		if envVar.SecretName != "" {
			secretEnvVars = append(secretEnvVars, envVar)
		} else {
			envVars = append(envVars, fmt.Sprintf("%s=%s", envVar.Name, envVar.Value))
		}
	}

	// Process secret environment variables
	if len(secretEnvVars) > 0 {
		db, err := storage.New()
		if err != nil {
			return result, fmt.Errorf("failed to create database: %w", err)
		}
		defer db.Close()
		identity, err := secrets.GetAgeIdentity()
		if err != nil {
			return result, fmt.Errorf("failed to get age identity: %w", err)
		}
		for _, secretEnvVar := range secretEnvVars {
			encryptedValue, err := db.GetSecretEncryptedValue(secretEnvVar.SecretName)
			if err != nil {
				return result, fmt.Errorf("failed to get encrypted secret value: %w", err)
			}
			decryptedValue, err := secrets.Decrypt(encryptedValue, identity)
			if err != nil {
				return result, fmt.Errorf("failed to decrypt secret value: %w", err)
			}
			envVars = append(envVars, fmt.Sprintf("%s=%s", secretEnvVar.Name, decryptedValue))
		}
	}

	networkMode := container.NetworkMode(constants.DockerNetwork)
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
		envVars := append(envVars, fmt.Sprintf("%s=%d", constants.EnvVarReplicaID, i+1))
		containerConfig := &container.Config{
			Image:  imageRef,
			Labels: labels,
			Env:    envVars,
		}
		containerName := fmt.Sprintf("%s-haloy-%s", appConfig.Name, deploymentID)
		if *appConfig.Replicas > 1 {
			containerName += fmt.Sprintf("-replica-%d", i+1)
		}

		resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
		if err != nil {
			return result, fmt.Errorf("failed to create container: %w", err)
		}

		// Ensure the container is removed on error
		// This is important to avoid leaving dangling containers in case of failure.
		// We use a deferred function to ensure cleanup happens even if the function exits early.
		defer func() {
			if err != nil && resp.ID != "" {
				// Try to remove container on error
				removeErr := cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
				if removeErr != nil {
					fmt.Printf("Failed to clean up container after error: %v\n", removeErr)
				}
			}
		}()

		if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
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

func StopContainers(ctx context.Context, cli *client.Client, logger *slog.Logger, appName, ignoreDeploymentID string) (stoppedIDs []string, err error) {
	// Get all containers including stopped ones to be thorough
	containerList, err := GetAppContainers(ctx, cli, true, appName)
	if err != nil {
		return stoppedIDs, err
	}

	// Filter containers that need to be stopped
	var containersToStop []container.Summary
	for _, containerInfo := range containerList {
		deploymentID := containerInfo.Labels[config.LabelDeploymentID]
		if deploymentID != ignoreDeploymentID {
			containersToStop = append(containersToStop, containerInfo)
		}
	}

	if len(containersToStop) == 0 {
		logger.Debug("No containers found to stop", "app", appName)
		return stoppedIDs, nil
	}

	logger.Info("Found containers to stop", "app", appName, "count", len(containersToStop))

	// Create a context with reasonable timeout for the entire operation
	stopCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	// For small numbers of containers, sequential is simpler and more reliable
	if len(containersToStop) <= 3 {
		return stopContainersSequential(stopCtx, cli, logger, containersToStop)
	}

	// For larger numbers, use controlled concurrency
	return stopContainersConcurrent(stopCtx, cli, logger, containersToStop)
}

func stopContainersSequential(ctx context.Context, cli *client.Client, logger *slog.Logger, containers []container.Summary) ([]string, error) {
	var stoppedIDs []string
	var errors []error

	for _, containerInfo := range containers {
		deploymentID := containerInfo.Labels[config.LabelDeploymentID]

		logger.Info("Stopping container", "container_id", helpers.SafeIDPrefix(containerInfo.ID), "deployment_id", deploymentID)

		if err := stopSingleContainer(ctx, cli, logger, containerInfo.ID); err != nil {
			logger.Error("Failed to stop container", "container_id", helpers.SafeIDPrefix(containerInfo.ID), "error", err)
			errors = append(errors, err)
		} else {
			logger.Info("Successfully stopped container", "container_id", helpers.SafeIDPrefix(containerInfo.ID))
			stoppedIDs = append(stoppedIDs, containerInfo.ID)
		}
	}

	var err error
	if len(errors) > 0 {
		err = fmt.Errorf("failed to stop %d out of %d containers", len(errors), len(containers))
	}

	return stoppedIDs, err
}

func stopContainersConcurrent(ctx context.Context, cli *client.Client, logger *slog.Logger, containers []container.Summary) ([]string, error) {
	type result struct {
		containerID string
		error       error
	}

	resultChan := make(chan result, len(containers))
	semaphore := make(chan struct{}, 3) // Limit to 3 concurrent stops

	// Start all goroutines
	for _, containerInfo := range containers {
		go func(container container.Summary) {
			semaphore <- struct{}{}        // Acquire
			defer func() { <-semaphore }() // Release

			deploymentID := container.Labels[config.LabelDeploymentID]
			logger.Info("Stopping container", "container_id", helpers.SafeIDPrefix(container.ID), "deployment_id", deploymentID)

			err := stopSingleContainer(ctx, cli, logger, container.ID)
			resultChan <- result{containerID: container.ID, error: err}
		}(containerInfo)
	}

	// Collect all results
	var stoppedIDs []string
	var errors []error

	for range len(containers) {
		res := <-resultChan
		if res.error != nil {
			logger.Error("Failed to stop container", "container_id", helpers.SafeIDPrefix(res.containerID), "error", res.error)
			errors = append(errors, res.error)
		} else {
			logger.Info("Successfully stopped container", "container_id", helpers.SafeIDPrefix(res.containerID))
			stoppedIDs = append(stoppedIDs, res.containerID)
		}
	}

	var err error
	if len(errors) > 0 {
		err = fmt.Errorf("failed to stop %d out of %d containers", len(errors), len(containers))
	}

	return stoppedIDs, err
}

func stopSingleContainer(ctx context.Context, cli *client.Client, logger *slog.Logger, containerID string) error {
	// First try a graceful stop
	timeout := 20
	stopOptions := container.StopOptions{Timeout: &timeout}

	err := cli.ContainerStop(ctx, containerID, stopOptions)
	if err == nil {
		return nil
	}

	logger.Warn("Graceful stop failed, attempting force kill", "container_id", helpers.SafeIDPrefix(containerID), "error", err)

	// If graceful stop fails, force kill
	killErr := cli.ContainerKill(ctx, containerID, "SIGKILL")
	if killErr != nil {
		return fmt.Errorf("both stop and kill failed - stop: %v, kill: %v", err, killErr)
	}

	return nil
}

type RemoveContainersResult struct {
	ID           string
	DeploymentID string
}

// RemoveContainers attempts to remove old containers for a given app and ignoring a specific deployment.
func RemoveContainers(ctx context.Context, cli *client.Client, logger *slog.Logger, appName, ignoreDeploymentID string) (removedIDs []string, err error) {
	containerList, err := GetAppContainers(ctx, cli, true, appName)
	if err != nil {
		return removedIDs, err
	}

	for _, containerInfo := range containerList {
		deploymentID := containerInfo.Labels[config.LabelDeploymentID]
		if deploymentID == ignoreDeploymentID {
			continue
		}

		err := cli.ContainerRemove(ctx, containerInfo.ID, container.RemoveOptions{Force: true})
		if err != nil {
			logger.Error("Error removing container %s: %v\n", helpers.SafeIDPrefix(containerInfo.ID), err)
		} else {
			removedIDs = append(removedIDs, containerInfo.ID)
		}
	}

	return removedIDs, nil
}

func HealthCheckContainer(ctx context.Context, cli *client.Client, logger *slog.Logger, containerID string, initialWaitTime ...time.Duration) error {
	// Check if container is running - wait up to 30 seconds for it to start
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var containerInfo container.InspectResponse
	var err error

	// Wait for container to be running
	for {
		containerInfo, err = cli.ContainerInspect(startCtx, containerID)
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
				containerInfo, err = cli.ContainerInspect(healthCtx, containerID)
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

		// If container has healthcheck and it's healthy, we can skip our manual check@
		switch containerInfo.State.Health.Status {
		case "healthy":
			logger.Debug("Container is healthy according to Docker healthcheck", "container_id", helpers.SafeIDPrefix(containerID))
			return nil
		case "starting":
			logger.Info("Container is still starting, falling back to manual health check", "container_id", helpers.SafeIDPrefix(containerID))
		case "unhealthy":
			if len(containerInfo.State.Health.Log) > 0 {
				lastLog := containerInfo.State.Health.Log[len(containerInfo.State.Health.Log)-1]
				return fmt.Errorf("container %s is unhealthy: %s", helpers.SafeIDPrefix(containerID), lastLog.Output)
			}
			return fmt.Errorf("container %s is unhealthy according to Docker healthcheck", helpers.SafeIDPrefix(containerID))
		default:
			return fmt.Errorf("container %s health status unknown: %s", helpers.SafeIDPrefix(containerID), containerInfo.State.Health.Status)
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

	targetIP, err := ContainerNetworkIP(containerInfo, constants.DockerNetwork)
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
			logger.Info("Retrying health check...", "backoff", backoff, "attempt", retry+1, "max_retries", maxRetries)
			time.Sleep(backoff)
			backoff *= 2 // Exponential backoff
		}

		req, err := http.NewRequestWithContext(ctx, "GET", healthCheckURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create health check request: %w", err)
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			logger.Warn("Health check attempt failed", "error", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		logger.Warn("Health check returned error status", "status_code", resp.StatusCode, "response", string(bodyBytes))
	}

	return fmt.Errorf("container %s failed health check after %d attempts", helpers.SafeIDPrefix(containerID), maxRetries)
}

// GetAppContainers returns a slice of container summaries filtered by labels.
//
// Parameters:
//   - ctx: the context for the Docker API requests.
//   - cli: the Docker client used to interact with the Docker daemon.
//   - listAll: if true, the function returns all containers including stopped ones;
//     if false, only running containers are returned.
//   - appName: if not empty, only containers associated with the given app name are returned.
//
// Returns:
//   - A slice of container summaries.
//   - An error if something went wrong during the container listing.
func GetAppContainers(ctx context.Context, cli *client.Client, listAll bool, appName string) ([]container.Summary, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelRole, config.AppLabelRole))
	if appName != "" {
		filterArgs.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, appName))
	}
	containerList, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filterArgs,
		All:     listAll, // If all is true, include stopped containers
	})
	if err != nil {
		if appName != "" {
			return nil, fmt.Errorf("failed to list containers for app %s: %w", appName, err)
		} else {
			return nil, fmt.Errorf("failed to list containers: %w", err)
		}
	}

	return containerList, nil
}

// ContainerNetworkInfo extracts the container's IP address
func ContainerNetworkIP(containerInfo container.InspectResponse, networkName string) (string, error) {
	// Add more detailed logging to help debug
	if containerInfo.State == nil {
		return "", fmt.Errorf("container state is nil")
	}

	if !containerInfo.State.Running {
		exitCode := 0
		if containerInfo.State.ExitCode != 0 {
			exitCode = containerInfo.State.ExitCode
		}
		return "", fmt.Errorf("container is not running (status: %s, exit code: %d)", containerInfo.State.Status, exitCode)
	}

	if _, exists := containerInfo.NetworkSettings.Networks[networkName]; !exists {
		var availableNetworks []string // List available networks for debugging
		for netName := range containerInfo.NetworkSettings.Networks {
			availableNetworks = append(availableNetworks, netName)
		}
		return "", fmt.Errorf("network '%s' not found, available networks: %v", networkName, availableNetworks)
	}

	ipAddress := containerInfo.NetworkSettings.Networks[networkName].IPAddress
	if ipAddress == "" {
		return "", fmt.Errorf("container has no IP address on network '%s'", networkName)
	}

	return ipAddress, nil
}

// checkImagePlatformCompatibility verifies the image platform matches the host
func checkImagePlatformCompatibility(ctx context.Context, cli *client.Client, imageRef string) error {
	imageInspect, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("failed to inspect image %s: %w", imageRef, err)
	}

	hostInfo, err := cli.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get host info: %w", err)
	}

	imagePlatform := imageInspect.Architecture
	hostPlatform := hostInfo.Architecture

	platformMap := map[string]string{
		"x86_64":  "amd64",
		"aarch64": "arm64",
		"armv7l":  "arm",
	}

	if normalized, exists := platformMap[imagePlatform]; exists {
		imagePlatform = normalized
	}
	if normalized, exists := platformMap[hostPlatform]; exists {
		hostPlatform = normalized
	}

	if imagePlatform != hostPlatform {
		return fmt.Errorf(
			"image built for %s but host is %s. "+
				"Rebuild the image for the correct platform or use docker buildx with --platform flag",
			imagePlatform, hostPlatform,
		)
	}

	return nil
}
