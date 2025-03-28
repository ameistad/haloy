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

func EnsureServicesIsRunning(dockerClient *client.Client, ctx context.Context) error {
	requiredServices := []string{"haloy-haproxy", "haloy-manager"}

	// Check if containers are running
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Track which services are running and healthy
	runningServices := make(map[string]bool)
	for _, container := range containers {
		for _, name := range container.Names {
			// Container names in the API response are prefixed with '/'
			cleanName := strings.TrimPrefix(name, "/")
			for _, service := range requiredServices {
				if cleanName == service {
					// Check if the container is running
					isRunning := container.State == "running"

					// Check if the container is healthy (if it has health check)
					isHealthy := true
					if container.Status != "" && strings.Contains(container.Status, "unhealthy") {
						isHealthy = false
					}

					runningServices[service] = isRunning && isHealthy
				}
			}
		}
	}

	// If all required services are running and healthy, return early
	allRunning := true
	for _, service := range requiredServices {
		if !runningServices[service] {
			allRunning = false
			break
		}
	}

	if allRunning {
		return nil
	}

	// Get docker-compose file path
	dockerComposeFilePath, err := config.ServicesDockerComposeFilePath()
	if err != nil {
		return fmt.Errorf("failed to get docker-compose file path: %w", err)
	}

	// Check if the docker-compose file exists
	if _, err := os.Stat(dockerComposeFilePath); os.IsNotExist(err) {
		return fmt.Errorf("docker-compose file not found at %s: %w", dockerComposeFilePath, err)
	}

	// Get the directory of the docker-compose file
	composeDir := filepath.Dir(dockerComposeFilePath)

	// Start the containers according to docker-compose
	// For simplicity, we'll use the docker command to run docker-compose
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", dockerComposeFilePath, "up", "-d")
	cmd.Dir = composeDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start services: %w\nOutput: %s", err, string(output))
	}
	return nil
}
