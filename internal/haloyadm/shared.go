package haloyadm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/ui"
)

// startHaloyManager runs the docker command to start haloy-manager.
func startHaloyManager(ctx context.Context, dataDir, configDir string, devMode bool) error {
	image := "ghcr.io/ameistad/haloy-manager:latest"
	if devMode {
		image = "haloy-manager:latest" // Use local image in dev mode
	}
	cmd := exec.CommandContext(ctx, "docker", "run",
		"--detach",
		"--env-file", fmt.Sprintf("%s/.env", configDir),
		"--name", config.HaloyManagerContainerName,
		"--volume", fmt.Sprintf("%s:/haloy-config:ro", configDir),
		"--volume", fmt.Sprintf("%s/haproxy-config:/haproxy-config:rw", dataDir),
		"--volume", fmt.Sprintf("%s/cert-storage:/cert-storage:rw", dataDir),
		"--volume", "/var/run/docker.sock:/var/run/docker.sock:rw",
		"--user", "root",
		"--publish", "127.0.0.1:8080:8080",
		"--publish", "127.0.0.1:9999:9999",
		"--label", "dev.haloy.role=manager",
		"--restart", "unless-stopped",
		"--network", "haloy-public",
		image,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			ui.Error("Failed to start haloy-manager: %s", stderr.String())
		}
		return fmt.Errorf("failed to start haloy-manager: %w", err)
	}

	if devMode {
		ui.Info("Starting haloy-manager in development mode using local image: %s", image)
	} else {
		ui.Info("Starting haloy-manager")
	}
	return nil
}

// startHaproxy runs the docker command to start haloy-haproxy.
func startHaproxy(ctx context.Context, dataDir string) error {
	cmd := exec.CommandContext(ctx, "docker", "run",
		"--detach",
		"--name", config.HAProxyContainerName,
		"--publish", "80:80",
		"--publish", "443:443",
		"--volume", fmt.Sprintf("%s/haproxy-config:/usr/local/etc/haproxy:ro", dataDir),
		"--volume", fmt.Sprintf("%s/cert-storage:/usr/local/etc/haproxy-certs:rw", dataDir),
		"--volume", fmt.Sprintf("%s/error-pages:/usr/local/etc/haproxy-errors:ro", dataDir),
		"--label", "dev.haloy.role=haproxy",
		"--user", "root",
		"--restart", "unless-stopped",
		"--network", "haloy-public",
		"haproxy:3.1.5",
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("failed to start haproxy: %s", stderr.String())
		}
		return fmt.Errorf("failed to start haproxy: %w", err)
	}

	return nil
}

// ContainerExists checks if a container with the given name exists (running or stopped).
func containerExists(ctx context.Context, containerName string) (bool, error) {
	// Use docker ps -a to list all containers filtered by name.
	// The filter "name=^/<name>$" ensures an exact match (Docker prepends a slash to container names).
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter", fmt.Sprintf("name=^/%s$", containerName), "--format", "{{.Names}}")
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	// If output contains the container name, then it exists.
	output := strings.TrimSpace(out.String())
	if output == containerName {
		return true, nil
	}

	// In case more than one container is returned, check each line.
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == containerName {
			return true, nil
		}
	}

	return false, nil
}

func startServices(ctx context.Context, dataDir, configDir string, devMode, restart bool) error {
	// Check if containers exist
	managerExists, err := containerExists(ctx, config.HaloyManagerContainerName)
	if err != nil {
		return fmt.Errorf("failed to check haloy-manager container: %w", err)
	}

	haproxyExists, err := containerExists(ctx, config.HAProxyContainerName)
	if err != nil {
		return fmt.Errorf("failed to check haloy-haproxy container: %w", err)
	}

	// If containers exist and restart flag is not set, return error
	if !restart {
		if managerExists {
			return fmt.Errorf("haloy-manager container already exists, use --restart flag to restart it")
		}
		if haproxyExists {
			return fmt.Errorf("haloy-haproxy container already exists, use --restart flag to restart it")
		}
	}

	// If restart flag is set, stop existing containers
	if restart {
		if managerExists {
			ui.Info("Stopping existing haloy-manager container")
			if err := stopContainer(ctx, config.HaloyManagerContainerName); err != nil {
				return fmt.Errorf("failed to stop existing haloy-manager: %w", err)
			}
		}
		if haproxyExists {
			ui.Info("Stopping existing haloy-haproxy container")
			if err := stopContainer(ctx, config.HAProxyContainerName); err != nil {
				return fmt.Errorf("failed to stop existing haloy-haproxy: %w", err)
			}
		}
	}

	// Start the services
	if err := startHaloyManager(ctx, dataDir, configDir, devMode); err != nil {
		return err
	}

	if err := startHaproxy(ctx, dataDir); err != nil {
		return err
	}

	return nil
}
