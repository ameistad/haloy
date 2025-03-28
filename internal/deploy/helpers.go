package deploy

import (
	"fmt"
	"os/exec"
	"strings"
)

// getContainerIP wraps the docker inspect call to retrieve a container's IP on a given network.
func GetContainerIP(containerID, networkName string) (string, error) {
	cmd := exec.Command("docker", "inspect", "--format",
		fmt.Sprintf("{{(index .NetworkSettings.Networks \"%s\").IPAddress}}", networkName),
		containerID)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get container IP: %w", err)
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", fmt.Errorf("no IP address found for container %s on network %s", containerID, networkName)
	}
	return ip, nil
}
