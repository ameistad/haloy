package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/client"
)

func NewClient() (*client.Client, context.Context, error) {
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	ctx := context.Background()
	_, err = dockerClient.Ping(ctx)
	if err != nil {
		dockerClient.Close()
		return nil, nil, fmt.Errorf("failed to connect to Docker daemon: %w", err)
	}

	return dockerClient, ctx, nil
}
