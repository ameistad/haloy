package docker

import (
	"context"
	"fmt"

	"github.com/ameistad/haloy/internal/config"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

func CreateNetwork(ctx context.Context, cli *client.Client) error {
	options := network.CreateOptions{
		Driver:     "bridge",
		Attachable: true,
		Labels: map[string]string{
			"created-by": "haloy",
		},
	}
	_, err := cli.NetworkCreate(ctx, config.DockerNetwork, options)
	if err != nil {
		return fmt.Errorf("failed to create Docker network: %w", err)
	}
	return nil
}

func EnsureNetwork(cli *client.Client, ctx context.Context) error {
	networks, err := cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list Docker networks: %w", err)
	}

	defaultNetworkExists := false
	for _, network := range networks {
		if network.Name == config.DockerNetwork {
			defaultNetworkExists = true
			break
		}
	}

	if !defaultNetworkExists {
		if err := CreateNetwork(ctx, cli); err != nil {
			return fmt.Errorf("failed to create Docker network: %w", err)
		}
	}
	return nil
}

// ContainerNetworkInfo extracts the container's IP address
func ContainerNetworkIP(container container.InspectResponse, networkName string) (string, error) {
	if _, exists := container.NetworkSettings.Networks[networkName]; !exists {
		return "", fmt.Errorf("specified network not found: %s", networkName)
	}
	if container.State == nil || !container.State.Running {
		return "", fmt.Errorf("container is not running")
	}
	ipAddress := container.NetworkSettings.Networks[networkName].IPAddress
	if ipAddress == "" {
		return "", fmt.Errorf("container has no IP address on the specified network: %s", networkName)
	}

	return ipAddress, nil
}
