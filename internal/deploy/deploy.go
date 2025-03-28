package deploy

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/docker/docker/client"
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

	if err := docker.BuildImage(ctx, dockerClient, imageName, appConfig); err != nil {
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

	containerID, deploymentID, err := runContainer(ctx, dockerClient, imageName, appConfig)
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
	ui.Info("New container '%s' started successfully.", containerID[:12])

	// Stop any old containers so that the reverse proxy routes traffic only to the new container.
	if err := StopOldContainers(appConfig.Name, containerID, deploymentID); err != nil {
		ui.Error("Failed to stop old containers: %w", err)
		return
	}

	// Prune old containers based on configuration.
	if err := PruneOldContainers(appConfig.Name, containerID, appConfig.KeepOldContainers); err != nil {
		ui.Error("Failed to prune old containers: %w", err)
		return
	}

	// Clean up old dangling images
	if err := PruneOldImages(appConfig.Name); err != nil {
		fmt.Printf("Warning: failed to prune old images: %v\n", err)
		// We don't return the error here as this is a non-critical step
	}

	fmt.Printf("Successfully deployed app '%s'. New deployment ID: %s\n", appConfig.Name, deploymentID)
}

func runContainer(ctx context.Context, dockerClient *client.Client, imageName string, appConfig *config.AppConfig) (string, string, error) {
	// deploymentID doesn't need to be a timestamp, but it needs to be incremented from the previous deployment.
	deploymentID := time.Now().Format("20060102150405")
	containerName := fmt.Sprintf("%s-haloy-%s", appConfig.Name, deploymentID)

	args := []string{"run", "-d", "--name", containerName, "--restart", "unless-stopped"}

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

	// Convert all labels to docker command arguments
	for k, v := range labels {
		args = append(args, "-l", fmt.Sprintf("%s=%s", k, v))
	}

	// Add environment variables.
	for k, v := range appConfig.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Add volumes.
	for _, vol := range appConfig.Volumes {
		args = append(args, "-v", vol)
	}

	if err := docker.EnsureNetwork(dockerClient, ctx); err != nil {
		return "", "", fmt.Errorf("failed to ensure Docker network exists: %w", err)
	}

	if err := docker.EnsureServicesIsRunning(dockerClient, ctx); err != nil {
		return "", "", fmt.Errorf("Failed to to start haproxy and haloy-manager: %w\n", err)
	}

	// Attach the container to the network.
	args = append(args, "--network", config.DockerNetwork)

	// Finally, set the image to run.
	args = append(args, imageName)

	cmd := exec.Command("docker", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	containerID := strings.TrimSpace(string(out))
	fmt.Printf("New container started with ID '%s' and name '%s'\n", containerID, containerName)
	return containerID, deploymentID, nil
}
