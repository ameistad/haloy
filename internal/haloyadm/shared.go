package haloyadm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ameistad/haloy/internal/config"
)

// startHaloyManager runs the docker command to start haloy-manager.
func startHaloyManager(ctx context.Context, dataDir, configDir string, devMode bool) error {
	image := "ghcr.io/ameistad/haloy-manager:latest"
	if devMode {
		image = "haloy-manager:latest" // Use local image in dev mode
	}

	// Get current user ID and group ID
	uid := os.Getuid()
	gid := os.Getgid()

	// Get Docker group ID - try to detect it dynamically
	dockerGID := getDockerGroupID()

	cmd := exec.CommandContext(ctx, "docker", "run",
		"--detach",
		"--env-file", filepath.Join(configDir, ".env"),
		"--name", config.ManagerContainerName,
		"--publish", fmt.Sprintf("127.0.0.1:%s:%s", config.CertificatesHTTPProviderPort, config.CertificatesHTTPProviderPort),
		"--publish", fmt.Sprintf("127.0.0.1:%s:%s", config.APIServerPort, config.APIServerPort),
		"--volume", fmt.Sprintf("%s:%s:ro", configDir, config.HaloyConfigPath), // config directory
		"--volume", fmt.Sprintf("%s%s:%s:rw", dataDir, config.HAProxyConfigPath, config.HAProxyConfigPath), // haproxy config directory,
		"--volume", fmt.Sprintf("%s%s:%s:rw", dataDir, config.CertificatesStoragePath, config.CertificatesStoragePath), // cert storage directory
		"--volume", "/var/run/docker.sock:/var/run/docker.sock:rw",
		"--user", fmt.Sprintf("%d:%d", uid, gid), // Run as current user
		"--group-add", dockerGID,
		"--label", fmt.Sprintf("%s=%s", config.LabelRole, config.ManagerLabelRole),
		"--restart", "unless-stopped",
		"--network", config.DockerNetwork,
		image,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("failed to start haloy-manager: %s", stderr.String())
		}
		return fmt.Errorf("failed to start haloy-manager: %w", err)
	}
	return nil
}

// Helper function to get Docker group ID dynamically
func getDockerGroupID() string {
	// First try environment variable
	if gid := os.Getenv("DOCKER_GID"); gid != "" {
		return gid
	}

	// Try to get it from getent command
	cmd := exec.Command("getent", "group", "docker")
	output, err := cmd.Output()
	if err == nil {
		// Parse output like "docker:x:999:user1,user2"
		parts := strings.Split(strings.TrimSpace(string(output)), ":")
		if len(parts) >= 3 {
			return parts[2] // The GID
		}
	}

	// Fall back to common default
	return "999"
}

// startHaproxy runs the docker command to start haloy-haproxy.
func startHaproxy(ctx context.Context, dataDir string) error {
	cmd := exec.CommandContext(ctx, "docker", "run",
		"--detach",
		"--name", config.HAProxyContainerName,
		"--publish", "80:80",
		"--publish", "443:443",
		"--volume", fmt.Sprintf("%s%s:/usr/local/etc/haproxy:ro", dataDir, config.HAProxyConfigPath),
		"--volume", fmt.Sprintf("%s%s:/usr/local/etc/haproxy-certs:rw", dataDir, config.CertificatesStoragePath),
		"--volume", fmt.Sprintf("%s/error-pages:/usr/local/etc/haproxy-errors:ro", dataDir),
		"--label", fmt.Sprintf("%s=%s", config.LabelRole, config.HAProxyLabelRole),
		"--user", "root",
		"--restart", "unless-stopped",
		"--network", config.DockerNetwork,
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

// containerExists checks if a haloy container with the given role exists (running or stopped).
func containerExists(ctx context.Context, role string) (bool, error) {
	// Use docker ps -a to list containers filtered by haloy role label
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s=%s", config.LabelRole, role),
		"--format", "{{.Names}}")

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	// If there's any output, a container with this role exists
	output := strings.TrimSpace(out.String())
	return output != "", nil
}

func startServices(ctx context.Context, dataDir, configDir string, devMode, restart bool) error {
	// Check if containers exist
	managerExists, err := containerExists(ctx, config.ManagerLabelRole)
	if err != nil {
		return fmt.Errorf("failed to check haloy-manager container: %w", err)
	}

	haproxyExists, err := containerExists(ctx, config.HAProxyLabelRole)
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
			if err := stopContainer(ctx, config.ManagerLabelRole); err != nil {
				return fmt.Errorf("failed to stop existing haloy-manager: %w", err)
			}
		}
		if haproxyExists {
			if err := stopContainer(ctx, config.HAProxyLabelRole); err != nil {
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

func stopContainer(ctx context.Context, role string) error {
	// First, get the container name by role
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=%s=%s", config.LabelRole, role),
		"--format", "{{.Names}}")

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to list containers with role %s: %w", role, err)
	}

	// If no container found, nothing to stop
	output := strings.TrimSpace(out.String())
	if output == "" {
		return nil // No container to stop
	}

	// Get the first container name (should only be one per role)
	containerName := strings.Split(output, "\n")[0]
	containerName = strings.TrimSpace(containerName)

	// Now stop and remove the container
	cmd = exec.CommandContext(ctx, "docker", "rm", "-f", containerName)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			return fmt.Errorf("failed to stop and remove container %s: %s", containerName, stderr.String())
		}
		return fmt.Errorf("failed to stop and remove container %s: %w", containerName, err)
	}

	return nil
}

// EnsureNetworkCmd checks for the existence of the specified Docker network and creates it if it doesn't exist.
func ensureNetwork(ctx context.Context) error {
	// List networks filtering by name
	// The --format option outputs only the network names.
	cmdList := exec.CommandContext(ctx, "docker", "network", "ls", "--filter", fmt.Sprintf("name=%s", config.DockerNetwork), "--format", "{{.Name}}")
	var out bytes.Buffer
	cmdList.Stdout = &out
	if err := cmdList.Run(); err != nil {
		return fmt.Errorf("failed to list Docker networks: %w", err)
	}

	// Check if the network exists.
	networks := strings.Split(strings.TrimSpace(out.String()), "\n")
	networkExists := false
	for _, n := range networks {
		if n == config.DockerNetwork {
			networkExists = true
			break
		}
	}

	if networkExists {
		return nil // Already exists.
	}

	// Create the network if it doesn't exist.
	// Here we create a bridge network that is attachable and assign a label.
	cmdCreate := exec.CommandContext(ctx, "docker", "network", "create",
		"--driver", "bridge",
		"--attachable",
		"--label", "created-by=haloy",
		config.DockerNetwork,
	)
	if output, err := cmdCreate.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create Docker network: %w - output: %s", err, output)
	}

	return nil
}
