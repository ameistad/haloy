package docker

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

type ContainerRunResult struct {
	ContainerID  string
	DeploymentID string
}

func RunContainer(ctx context.Context, dockerClient *client.Client, imageName string, appConfig *config.AppConfig) (ContainerRunResult, error) {
	deploymentID := time.Now().Format("20060102150405")
	containerName := fmt.Sprintf("%s-haloy-%s", appConfig.Name, deploymentID)

	// Convert AppConfig to ContainerLabels
	cl := config.ContainerLabels{
		AppName:         appConfig.Name,
		DeploymentID:    deploymentID,
		Ignore:          false,
		ACMEEmail:       appConfig.ACMEEmail,
		Port:            appConfig.Port,
		HealthCheckPath: appConfig.HealthCheckPath,
		Domains:         appConfig.Domains,
	}
	// Add all labels at once by merging maps
	labels := cl.ToLabels()

	// Process environment variables
	var envVars []string
	decryptedEnvVars, err := config.DecryptEnvVars(appConfig.Env)
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("failed to decrypt environment variables: %w", err)
	}
	for _, v := range decryptedEnvVars {
		value, err := v.GetValue()
		if err != nil {
			return ContainerRunResult{}, fmt.Errorf("failed to get value for env var '%s': %w", v.Name, err)
		}
		envVars = append(envVars, fmt.Sprintf("%s=%s", v.Name, value))
	}

	// Prepare container configuration
	containerConfig := &container.Config{
		Image:  imageName,
		Labels: labels,
		Env:    envVars,
	}

	// Prepare host configuration - set restart policy and volumes to mount.
	hostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		Binds:         appConfig.Volumes,
	}

	// Ensure that the custom network and required services are running.
	if err := EnsureNetwork(dockerClient, ctx); err != nil {
		return ContainerRunResult{}, fmt.Errorf("failed to ensure Docker network exists: %w", err)
	}
	if err := EnsureServicesIsRunning(dockerClient, ctx); err != nil {
		return ContainerRunResult{}, fmt.Errorf("failed to ensure dependent services are running: %w", err)
	}

	// Attach the container to the predefined network
	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			config.DockerNetwork: {},
		},
	}

	// Create the container
	resp, err := dockerClient.ContainerCreate(ctx, containerConfig, hostConfig, networkingConfig, nil, containerName)
	if err != nil {
		return ContainerRunResult{}, fmt.Errorf("failed to create container: %w", err)
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

	// Start the container
	if err := dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return ContainerRunResult{}, fmt.Errorf("failed to start container: %w", err)
	}

	return ContainerRunResult{
		ContainerID:  resp.ID,
		DeploymentID: deploymentID,
	}, nil
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
			ui.Warning("Error stopping container %s: %v\n", containerInfo.ID[:12], err)
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

func RemoveContainers(params RemoveContainersParams) error {
	filter := filters.NewArgs()
	filter.Add("label", fmt.Sprintf("%s=%s", config.LabelAppName, params.AppName))

	containerList, err := params.DockerClient.ContainerList(params.Context, container.ListOptions{
		Filters: filter,
		All:     true,
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Create a slice to hold containers we're going to process
	type containerInfo struct {
		id           string
		deploymentID string
	}
	var containers []containerInfo

	// Filter out the container with IgnoreDeploymentID
	for _, c := range containerList {
		deploymentID := c.Labels[config.LabelDeploymentID]
		if deploymentID == params.IgnoreDeploymentID {
			ui.Info("Skipping container with ignored deployment ID: %s", deploymentID)
			continue
		}
		containers = append(containers, containerInfo{
			id:           c.ID,
			deploymentID: deploymentID,
		})
	}

	ui.Info("Found %d containers matching app name %s", len(containers), params.AppName)

	// Sort containers by deploymentID (newest/largest timestamp first)
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].deploymentID > containers[j].deploymentID
	})

	// Debug the sorting order
	for i, c := range containers {
		ui.Info("Container %d: ID=%s, DeploymentID=%s", i, c.id[:12], c.deploymentID)
	}

	// Skip newest containers according to NumberOfContainersToSkip
	containersToKeep := containers
	var containersToRemove []containerInfo

	if params.MaxContainersToKeep > 0 && len(containers) > params.MaxContainersToKeep {
		containersToKeep = containers[:params.MaxContainersToKeep]
		containersToRemove = containers[params.MaxContainersToKeep:]
	} else {
		// If we have fewer containers than the skip count, don't remove any
		containersToRemove = []containerInfo{}
	}

	ui.Info("Keeping %d newest containers, removing %d older containers",
		len(containersToKeep), len(containersToRemove))

	// Remove the remaining containers
	for _, c := range containersToRemove {
		ui.Info("Removing container %s with deployment ID %s", c.id[:12], c.deploymentID)
		err := params.DockerClient.ContainerRemove(params.Context, c.id, container.RemoveOptions{Force: true})
		if err != nil {
			ui.Warning("Error removing container %s: %v", c.id[:12], err)
		}
	}

	return nil
}
