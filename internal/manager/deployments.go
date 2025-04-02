package manager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"sort"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/manager/certificates"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type DeploymentInstance struct {
	IP   string
	Port string
}

type Deployment struct {
	Labels    *config.ContainerLabels
	Instances []DeploymentInstance
}

type DeploymentManager struct {
	DockerClient *client.Client
	// Store the previous hash so we can compare it with the new hash to see if anything has changed.
	previousHash string
	deployments  []Deployment
}

func NewDeploymentManager(dockerClient *client.Client) *DeploymentManager {
	return &DeploymentManager{
		DockerClient: dockerClient,
		deployments:  []Deployment{},
	}
}

func (dm *DeploymentManager) BuildDeployments(ctx context.Context) error {
	deploymentsMap := make(map[string]Deployment)
	containers, err := dm.DockerClient.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return err
	}

	for _, containerSummary := range containers {
		container, err := dm.DockerClient.ContainerInspect(ctx, containerSummary.ID)
		if err != nil {
			log.Printf("Failed to inspect container %s: %v", containerSummary.ID, err)
			continue
		}

		labels, err := config.ParseContainerLabels(container.Config.Labels)
		if err != nil {
			continue
		}

		ip, err := docker.ContainerNetworkIP(container, config.DockerNetwork)
		if err != nil {
			log.Printf("Failed to get IP address for container %s: %v", container.ID, err)
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
				// Replace the deployment if the new one has a higher deployment ID
				if deployment.Labels.DeploymentID < labels.DeploymentID {
					deploymentsMap[labels.AppName] = Deployment{Labels: labels, Instances: []DeploymentInstance{instance}}
				}
			}
		} else {
			deploymentsMap[labels.AppName] = Deployment{Labels: labels, Instances: []DeploymentInstance{instance}}
		}
	}

	// Convert map to slice
	var deployments []Deployment
	for _, deployment := range deploymentsMap {
		deployments = append(deployments, deployment)
	}

	dm.deployments = deployments
	return nil
}

func (dm *DeploymentManager) Deployments() []Deployment {
	return dm.deployments
}

func (dm *DeploymentManager) calculateHash() string {
	var b bytes.Buffer

	// Sort deployments by app name for consistency
	sort.Slice(dm.deployments, func(i, j int) bool {
		return dm.deployments[i].Labels.AppName < dm.deployments[j].Labels.AppName
	})

	for _, d := range dm.deployments {
		// Write app name and deployment ID
		b.WriteString(d.Labels.AppName)
		b.WriteString(d.Labels.DeploymentID)

		// Sort instances for consistency
		sort.Slice(d.Instances, func(i, j int) bool {
			if d.Instances[i].IP != d.Instances[j].IP {
				return d.Instances[i].IP < d.Instances[j].IP
			}
			return d.Instances[i].Port < d.Instances[j].Port
		})

		// Write instance information
		for _, i := range d.Instances {
			b.WriteString(i.IP)
			b.WriteString(i.Port)
		}

		// Write domains information
		for _, domain := range d.Labels.Domains {
			b.WriteString(domain.Canonical)
			for _, alias := range domain.Aliases {
				b.WriteString(alias)
			}
		}
	}

	// Calculate hash
	hash := sha256.Sum256(b.Bytes())
	return hex.EncodeToString(hash[:])
}

func (dm *DeploymentManager) HasChanged() bool {
	currentHash := dm.calculateHash()
	changed := currentHash != dm.previousHash

	// Update the hash if changed
	if changed {
		dm.previousHash = currentHash
	}

	return changed
}

func (dm *DeploymentManager) GetCertificateDomains() []certificates.DomainEmail {
	domains := make([]certificates.DomainEmail, 0)

	for _, deployment := range dm.deployments {
		for _, domain := range deployment.Labels.Domains {
			// Add canonical domain
			domains = append(domains, certificates.DomainEmail{
				Domain: domain.Canonical,
				Email:  deployment.Labels.ACMEEmail,
			})

			// Add all aliases
			for _, alias := range domain.Aliases {
				domains = append(domains, certificates.DomainEmail{
					Domain: alias,
					Email:  deployment.Labels.ACMEEmail,
				})
			}
		}
	}

	return domains
}
