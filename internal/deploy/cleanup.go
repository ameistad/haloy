package deploy

import (
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
)

func PruneOldContainers(appName, newContainerID string, keepCount int) error {
	out, err := exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("label=haloy.appName=%s", appName), "--format", "{{.ID}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to list containers: %w - output: %s", err, string(out))
	}

	ids := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(ids) == 0 || (len(ids) == 1 && ids[0] == "") {
		return nil
	}

	var containers []ContainerInfo
	for _, id := range ids {
		if id == "" {
			continue
		}
		labelOut, err := exec.Command("docker", "inspect", "--format", "{{ index .Config.Labels \"haloy.deployment\" }}", id).CombinedOutput()
		if err != nil {
			fmt.Printf("Error inspecting container %s for deployment label: %v\n", id, err)
			continue
		}

		depID := strings.TrimSpace(string(labelOut))
		// Validate deployment ID format (should be a timestamp like 20060102150405)
		if len(depID) != 14 || !isNumeric(depID) {
			fmt.Printf("Warning: Container %s has invalid deployment ID format: %s\n", id, depID)
		}

		containers = append(containers, ContainerInfo{ID: id, DeploymentID: depID})
	}

	var oldContainers []ContainerInfo
	for _, c := range containers {
		if c.ID == newContainerID {
			continue
		}
		oldContainers = append(oldContainers, c)
	}

	// Sort by deployment ID (newer ones first)
	sort.Slice(oldContainers, func(i, j int) bool {
		return oldContainers[i].DeploymentID > oldContainers[j].DeploymentID
	})

	if len(oldContainers) <= keepCount {
		fmt.Println("No extra containers to prune.")
		return nil
	}

	for _, c := range oldContainers[keepCount:] {
		fmt.Printf("Pruning container %s (deployment: %s)\n", c.ID, c.DeploymentID)
		out, err := exec.Command("docker", "rm", c.ID).CombinedOutput()
		if err != nil {
			fmt.Printf("Error pruning container %s: %v, details: %s\n", c.ID, err, string(out))
		}
	}
	return nil
}

func PruneOldImages(appName string) error {
	fmt.Println("Pruning dangling images...")

	// First, remove unused images related to this app
	listCmd := exec.Command("docker", "images", "--filter", fmt.Sprintf("reference=%s", appName), "--format", "{{.ID}}")
	output, err := listCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error listing images for %s: %w (%s)", appName, err, string(output))
	}

	imageIDs := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, id := range imageIDs {
		if id == "" {
			continue
		}

		// Check if the image is not being used
		inspectCmd := exec.Command("docker", "inspect", "--format", "{{.RepoTags}}", id)
		inspectOut, err := inspectCmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Warning: could not inspect image %s: %v\n", id, err)
			continue
		}

		// Skip the latest tag
		if strings.Contains(string(inspectOut), fmt.Sprintf("%s:latest", appName)) {
			continue
		}

		fmt.Printf("Removing old image: %s\n", id)
		removeCmd := exec.Command("docker", "rmi", id)
		removeOut, err := removeCmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Warning: could not remove image %s: %v (%s)\n", id, err, string(removeOut))
		}
	}

	// Then, prune dangling images (no tag) system-wide
	pruneCmd := exec.Command("docker", "image", "prune", "--force")
	pruneCmd.Stdout = io.Discard
	pruneCmd.Stderr = io.Discard
	if err := pruneCmd.Run(); err != nil {
		return fmt.Errorf("error pruning dangling images: %w", err)
	}

	return nil
}

// Helper function to check if a string contains only digits
func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
