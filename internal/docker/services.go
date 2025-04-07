package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// ServiceState represents the current state of the haloy services
type ServiceState string

const (
	// ServiceStateRunning indicates services were already running and healthy
	ServiceStateRunning ServiceState = "running"
	// ServiceStateStarted indicates services needed to be started
	ServiceStateStarted ServiceState = "started"
	// ServiceStatePartial indicates some services are running but not all
	ServiceStatePartial ServiceState = "partial"
)

// ServiceStatus contains detailed information about the services
type ServiceStatus struct {
	State           ServiceState
	RunningServices map[string]bool
	Details         string
}

func EnsureServicesIsRunning(dockerClient *client.Client, ctx context.Context) (ServiceStatus, error) {
	requiredRoles := []string{config.HAProxyLabelRole, config.ManagerLabelRole}
	status := ServiceStatus{
		RunningServices: make(map[string]bool),
	}

	// Check if containers are running
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return status, fmt.Errorf("failed to list containers: %w", err)
	}

	// Track which services are running and healthy
	for _, container := range containers {
		// Check if this container has a haloy role label
		if roleValue, hasRole := container.Labels[config.LabelRole]; hasRole {
			// Check if the container's role is one we're looking for
			for _, requiredRole := range requiredRoles {
				if roleValue == requiredRole {
					// Check if the container is running
					isRunning := container.State == "running"

					// Check if the container is healthy (if it has health check)
					isHealthy := true
					if container.Status != "" && strings.Contains(container.Status, "unhealthy") {
						isHealthy = false
					}

					status.RunningServices[roleValue] = isRunning && isHealthy
				}
			}
		}
	}

	// If all required roles are running and healthy, return early
	allRunning := true
	runningCount := 0
	for _, role := range requiredRoles {
		if status.RunningServices[role] {
			runningCount++
		} else {
			allRunning = false
		}
	}

	if allRunning {
		status.State = ServiceStateRunning
		status.Details = "All services are already running and healthy"
		return status, nil
	}

	if runningCount > 0 {
		status.State = ServiceStatePartial
		status.Details = fmt.Sprintf("%d of %d services are running", runningCount, len(requiredRoles))
	}

	// Rest of the function remains the same
	// Get docker-compose file path
	dockerComposeFilePath, err := config.ServicesDockerComposeFilePath()
	if err != nil {
		return status, fmt.Errorf("failed to get docker-compose file path: %w", err)
	}

	// Check if the docker-compose file exists
	if _, err := os.Stat(dockerComposeFilePath); os.IsNotExist(err) {
		return status, fmt.Errorf("docker-compose file not found at %s: %w", dockerComposeFilePath, err)
	}

	// Get the directory of the docker-compose file
	composeDir := filepath.Dir(dockerComposeFilePath)

	// Start the containers according to docker-compose
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", dockerComposeFilePath, "up", "-d")
	cmd.Dir = composeDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		status.Details = string(output)
		return status, fmt.Errorf("failed to start services: %w", err)
	}

	status.State = ServiceStateStarted
	status.Details = "Services started successfully"
	return status, nil
}
