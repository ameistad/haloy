package haloyadm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/apiclient"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/joho/godotenv"
)

// startHaloyManager runs the docker command to start haloy-manager.
func startHaloyManager(ctx context.Context, dataDir, configDir string, devMode bool, debug bool) error {
	image := "ghcr.io/ameistad/haloy-manager:latest"
	if devMode {
		image = "haloy-manager:latest" // Use local image in dev mode
	}

	uid := os.Getuid()
	gid := os.Getgid()
	dockerGID := getDockerGroupID()

	args := []string{
		"run",
		"--detach",
		"--name", constants.ManagerContainerName,
		"--publish", fmt.Sprintf("127.0.0.1:%s:%s", constants.CertificatesHTTPProviderPort, constants.CertificatesHTTPProviderPort),
		"--publish", fmt.Sprintf("127.0.0.1:%s:%s", constants.APIServerPort, constants.APIServerPort),
		"--volume", fmt.Sprintf("%s:%s:ro", configDir, configDir), // /etc/haloy or ~/.config/haloy
		"--volume", fmt.Sprintf("%s:%s:rw", dataDir, dataDir), // /var/lib/haloy or ~/.local/share/haloy
		"--volume", "/var/run/docker.sock:/var/run/docker.sock:rw",
		"--user", fmt.Sprintf("%d:%d", uid, gid),
		"--group-add", dockerGID,
		"--label", fmt.Sprintf("%s=%s", config.LabelRole, config.ManagerLabelRole),
		"--restart", "unless-stopped",
		"--network", constants.DockerNetwork,
		// Path environment variables so we can use paths functions and get the same results as on the host system.
		"--env", fmt.Sprintf("%s=%s", constants.EnvVarDataDir, dataDir),
		"--env", fmt.Sprintf("%s=%s", constants.EnvVarConfigDir, configDir),
		"--env", fmt.Sprintf("%s=%s", constants.EnvVarSystemInstall, fmt.Sprintf("%t", config.IsSystemMode())),
	}

	// using godotenv to add env variables from .env because --env-file does not support quotes in values.
	envFile := filepath.Join(configDir, constants.ConfigEnvFileName)
	env, err := godotenv.Read(envFile)
	if err != nil {
		return fmt.Errorf("failed to read env file: %w", err)
	}
	for key, value := range env {
		args = append(args[:2], append([]string{"--env", fmt.Sprintf("%s=%s", key, value)}, args[2:]...)...)
	}

	if debug {
		args = append(args[:2], append([]string{"--env", fmt.Sprintf("%s=true", constants.EnvVarDebug)}, args[2:]...)...)
	}

	args = append(args, image)

	cmd := exec.CommandContext(ctx, "docker", args...)

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

// startHAProxy runs the docker command to start HAProxy.
func startHAProxy(ctx context.Context, dataDir string) error {
	cmd := exec.CommandContext(ctx, "docker", "run",
		"--detach",
		"--name", constants.HAProxyContainerName,
		"--publish", "80:80",
		"--publish", "443:443",
		"--volume", fmt.Sprintf("%s/%s:/usr/local/etc/haproxy:ro", dataDir, constants.HAProxyConfigDir),
		"--volume", fmt.Sprintf("%s/%s:/usr/local/etc/haproxy-certs:rw", dataDir, constants.CertStorageDir),
		"--volume", fmt.Sprintf("%s/error-pages:/usr/local/etc/haproxy-errors:ro", dataDir),
		"--label", fmt.Sprintf("%s=%s", config.LabelRole, config.HAProxyLabelRole),
		// Running as root is necessary for privileged ports 80 and 443.
		"--user", "root",
		"--user", "root",
		"--restart", "unless-stopped",
		"--network", constants.DockerNetwork,
		fmt.Sprintf("haproxy:%s", constants.HAProxyVersion),
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

func startServices(ctx context.Context, dataDir, configDir string, devMode, restart, debug bool) error {
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
			ui.Info("Manager is already running. Restarting...")
			if err := stopContainer(ctx, config.ManagerLabelRole); err != nil {
				return fmt.Errorf("failed to stop existing haloy-manager: %w", err)
			}
		}
		if haproxyExists {
			ui.Info("HAProxy is already running. Restarting...")
			if err := stopContainer(ctx, config.HAProxyLabelRole); err != nil {
				return fmt.Errorf("failed to stop existing haloy-haproxy: %w", err)
			}
		}
	}

	// Start the services
	if err := startHaloyManager(ctx, dataDir, configDir, devMode, debug); err != nil {
		return err
	}

	if err := startHAProxy(ctx, dataDir); err != nil {
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
	cmdList := exec.CommandContext(ctx, "docker", "network", "ls", "--filter", fmt.Sprintf("name=%s", constants.DockerNetwork), "--format", "{{.Name}}")
	var out bytes.Buffer
	cmdList.Stdout = &out
	if err := cmdList.Run(); err != nil {
		return fmt.Errorf("failed to list Docker networks: %w", err)
	}

	// Check if the network exists.
	networks := strings.Split(strings.TrimSpace(out.String()), "\n")
	networkExists := false
	for _, n := range networks {
		if n == constants.DockerNetwork {
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
		constants.DockerNetwork,
	)
	if output, err := cmdCreate.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create Docker network: %w - output: %s", err, output)
	}

	return nil
}

// streamManagerInitLogs waits for the API to become available and streams initialization logs
func streamManagerInitLogs(ctx context.Context, token string) error {
	apiURL := fmt.Sprintf("http://localhost:%s", constants.APIServerPort)
	api := apiclient.New(apiURL, token)

	ui.Info("Connecting to manager API...")

	waitCtx, waitCancel := context.WithTimeout(ctx, apiWaitTimeout)
	defer waitCancel()

	if err := waitForAPI(waitCtx, api); err != nil {
		return fmt.Errorf("manager API not available: %w", err)
	}

	ui.Info("Streaming manager initialization logs...")

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()
	return api.StreamManagerInitLogs(streamCtx)
}

// waitForAPI polls the API health endpoint until it's available
func waitForAPI(ctx context.Context, api *apiclient.APIClient) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Try to check API health (without auth since health endpoint is public)
			healthCtx, healthCancel := context.WithTimeout(ctx, 2*time.Second)
			err := api.HealthCheck(healthCtx)
			healthCancel()

			if err == nil {
				return nil // API is available
			}

			// Continue polling if API is not ready yet
		}
	}
}

// generateAPIToken creates a secure random API token
func generateAPIToken() (string, error) {
	tokenBytes := make([]byte, apiTokenLength)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	// Validate generated token
	if len(token) != apiTokenLength*2 {
		return "", fmt.Errorf("generated token has unexpected length: got %d, expected %d",
			len(token), apiTokenLength*2)
	}

	return token, nil
}
