package haloyadm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/ui"
	"github.com/spf13/cobra"
)

const (
	startTimeout = 5 * time.Minute
)

func StartCmd() *cobra.Command {
	var devMode bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the haloy services",
		Long:  "Start the haloy services, including HAProxy and haloy-manager.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {

			ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
			defer cancel()

			if err := startHaloyManager(ctx, devMode); err != nil {
				ui.Error("Failed to start haloy-manager: %v", err)
				return
			}

			// Then start haloy-haproxy.
			if err := startHaproxy(ctx); err != nil {
				ui.Error("Failed to start haloy-haproxy: %v", err)
				return
			}
			ui.Success("Haloy services started successfully")
		},
	}
	cmd.Flags().BoolVar(&devMode, "dev", false, "Start in development mode using the local haloy-manager image")
	return cmd
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

// startHaloyManager runs the docker command to start haloy-manager.
func startHaloyManager(ctx context.Context, devMode bool) error {

	image := "ghcr.io/ameistad/haloy-manager:latest"
	if devMode {
		image = "haloy-manager:latest" // Use local image in dev mode
	}
	cmd := exec.CommandContext(ctx, "docker", "run",
		"--detach",
		"--env-file", "./.env",
		"--name", "haloy-manager",
		"--volume", "./haproxy-config:/haproxy-config:rw",
		"--volume", "./cert-storage:/cert-storage:rw",
		"--volume", "/var/run/docker.sock:/var/run/docker.sock:rw",
		"--publish", "127.0.0.1:8080:8080",
		"--publish", "127.0.0.1:9999:9999",
		"--label", "dev.haloy.role=manager",
		"--restart", "unless-stopped",
		"--network", "haloy-public",
		image,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if devMode {
		ui.Info("Starting haloy-manager in development mode using local image: %s", image)
	} else {
		ui.Info("Starting haloy-manager")
	}
	return cmd.Run()
}

// startHaproxy runs the docker command to start haloy-haproxy.
func startHaproxy(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "run",
		"--detach",
		"--name", "haloy-haproxy",
		"--publish", "80:80",
		"--publish", "443:443",
		"--volume", "./haproxy-config:/usr/local/etc/haproxy:ro",
		"--volume", "./cert-storage:/usr/local/etc/haproxy-certs:rw",
		"--volume", "./error-pages:/usr/local/etc/haproxy-errors:ro",
		"--label", "dev.haloy.role=haproxy",
		"--restart", "unless-stopped",
		"--network", "haloy-public",
		// Note: docker run does not natively support "depends_on". We expect haloy-manager to be running.
		"haproxy:3.1.5",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	ui.Info("Starting haloy-haproxy")
	return cmd.Run()
}
