package docker

import (
	"context"
	"fmt"

	"github.com/ameistad/haloy/internal/config"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

func CreateNetwork(dockerClient *client.Client, ctx context.Context) error {
	options := network.CreateOptions{
		Driver:     "bridge",
		Attachable: true,
		Labels: map[string]string{
			"created-by": "haloy",
		},
	}
	_, err := dockerClient.NetworkCreate(ctx, config.DockerNetwork, options)
	if err != nil {
		return fmt.Errorf("failed to create Docker network: %w", err)
	}
	return nil
}

func EnsureNetwork(dockerClient *client.Client, ctx context.Context) error {
	networks, err := dockerClient.NetworkList(ctx, network.ListOptions{})
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
		if err := CreateNetwork(dockerClient, ctx); err != nil {
			return fmt.Errorf("failed to create Docker network: %w", err)
		}
	}
	return nil
}
