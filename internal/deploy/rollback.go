package deploy

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/ameistad/haloy/internal/config"
)

func RollbackToContainer(currentContainerID, targetContainerID string, healthCheckPath string) error {
	fmt.Printf("Starting target container: %s\n", targetContainerID)
	if err := exec.Command("docker", "start", targetContainerID).Run(); err != nil {
		return fmt.Errorf("failed to start target container %s: %w", targetContainerID, err)
	}

	// check health of target container with HealthCheckContainer
	if err := HealthCheckContainer(targetContainerID, healthCheckPath); err != nil {
		return fmt.Errorf("target container %s is not healthy: %w", targetContainerID, err)
	}

	fmt.Printf("Stopping current container: %s\n", currentContainerID)
	if err := exec.Command("docker", "stop", currentContainerID).Run(); err != nil {
		return fmt.Errorf("failed to stop current container %s: %w", currentContainerID, err)
	}

	return nil
}

func SortedContainerInfo(appConfig *config.AppConfig) ([]ContainerInfo, error) {
	out, err := exec.Command("docker", "ps", "-a",
		"--filter", fmt.Sprintf("label=haloy.appName=%s", appConfig.Name),
		"--format", "{{.ID}}").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	ids := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(ids) < 2 {
		return nil, fmt.Errorf("no previous container found to rollback to")
	}

	var containers []ContainerInfo

	// Inspect each container for its deployment timestamp.
	for _, id := range ids {
		if id == "" {
			continue
		}
		labelOut, err := exec.Command("docker", "inspect",
			"--format", "{{ index .Config.Labels \"haloy.deployment\" }}", id).Output()
		if err != nil {
			fmt.Printf("Error inspecting container %s: %v\n", id, err)
			continue
		}
		deploymentLabel := strings.TrimSpace(string(labelOut))
		containers = append(containers, ContainerInfo{
			ID:           id,
			DeploymentID: deploymentLabel,
		})
	}
	return containers, nil
}
