package manager

import (
	"context"
	"fmt"
	"log"

	"github.com/ameistad/haloy/internal/config"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// TODO:
// - create a DeploymentManager struct with a DockerClient field
// - add a BuildDeployments method to the DeploymentManager struct
// - create a calculateHash method on the DeploymentManager struct
// - create a previousHash field on the DeploymentManager struct
// - create a HasChanged method on the DeploymentManager struct
// - creat a GetCertificateDomains method on the DeploymentManager struct which returns a slice of certificates.DomainEmail

type DeploymentInstance struct {
	IP   string
	Port string
}

type Deployment struct {
	Labels    *config.ContainerLabels
	Instances []DeploymentInstance
}

func CreateDeployments(ctx context.Context, dockerClient *client.Client) ([]Deployment, error) {
	deploymentsMap := make(map[string]Deployment)
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, err
	}

	for _, containerSummary := range containers {
		container, err := dockerClient.ContainerInspect(ctx, containerSummary.ID)
		if err != nil {
			log.Printf("Failed to inspect container %s: %v", containerSummary.ID, err)
			continue
		}

		labels, err := config.ParseContainerLabels(container.Config.Labels)
		if err != nil {
			continue
		}

		ip, err := ContainerNetworkIP(container, config.DockerNetwork)
		if err != nil {
			log.Printf("Failed to get IP address IP for container %s: %v", container.ID, err)
			continue
		}

		var port string
		if labels.Port != "" {
			port = labels.Port
		} else {
			port = config.DefaultContainerPort
		}

		instance := DeploymentInstance{IP: ip, Port: port}

		if deployment, exists := deploymentsMap[labels.AppName]; exists {
			// There is a appName match, check if the deployment ID matches.
			if deployment.Labels.DeploymentID == labels.DeploymentID {
				deployment.Instances = append(deployment.Instances, instance)
				deploymentsMap[labels.AppName] = deployment
			} else {
				// Replace the deployment if the new one has a higher deployment ID indicating a newer deployment.
				if deployment.Labels.DeploymentID < labels.DeploymentID {
					deploymentsMap[labels.AppName] = Deployment{Labels: labels, Instances: []DeploymentInstance{instance}}
				}
			}
		} else {
			deploymentsMap[labels.AppName] = Deployment{Labels: labels, Instances: []DeploymentInstance{instance}}
		}
	}
	var deployments []Deployment
	for _, deployment := range deploymentsMap {
		deployments = append(deployments, deployment)
	}
	return deployments, nil
}

// ContainerNetworkInfo extracts the container's IP address and exposed ports
func ContainerNetworkIP(container types.ContainerJSON, networkName string) (string, error) {
	// Check if the network exists
	if _, exists := container.NetworkSettings.Networks[networkName]; !exists {
		return "", fmt.Errorf("specified network not found: %s", networkName)
	}

	// Get IP address from the specified network
	ipAddress := container.NetworkSettings.Networks[networkName].IPAddress
	if ipAddress == "" {
		return "", fmt.Errorf("container has no IP address on the specified network: %s", networkName)
	}

	return ipAddress, nil
}
