package manager

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/docker/docker/client"
)

type DeploymentInstance struct {
	ContainerID string
	IP          string
	Port        string
}

type Deployment struct {
	Labels    *config.ContainerLabels
	Instances []DeploymentInstance
}

// FailedContainerInfo holds information about a container that failed processing.
type FailedContainerInfo struct {
	ContainerID string
	Error       string
	Labels      *config.ContainerLabels
}

type DeploymentManager struct {
	cli *client.Client
	// deployments is a map of appName to Deployment, key is the app name.
	deployments      map[string]Deployment
	compareResult    compareResult
	deploymentsMutex sync.RWMutex
}

func NewDeploymentManager(cli *client.Client) *DeploymentManager {
	return &DeploymentManager{
		cli:         cli,
		deployments: make(map[string]Deployment),
	}
}

// BuildDeployments scans all running Docker containers with the app label and builds a map of
// current deployments in the system. It compares the new deployment state with the previous state
// to determine if any changes have occurred (additions, removals, or updates to deployments).
// Returns true if the deployment state has changed, along with any error encountered.
func (dm *DeploymentManager) BuildDeployments(ctx context.Context, logger *slog.Logger) (hasChanged bool, failedContainers []FailedContainerInfo, err error) {
	newDeployments := make(map[string]Deployment)
	containers, err := docker.GetAppContainers(ctx, dm.cli, false, "")
	if err != nil {
		return hasChanged, failedContainers, fmt.Errorf("failed to get containers: %w", err)
	}

	for _, containerSummary := range containers {
		container, err := dm.cli.ContainerInspect(ctx, containerSummary.ID)
		if err != nil {
			logger.Error("Failed to inspect container", "container_id", containerSummary.ID, "error", err)
			failedContainers = append(failedContainers, FailedContainerInfo{
				ContainerID: containerSummary.ID,
				Error:       err.Error(),
				Labels:      nil, // No labels if inspection failed
			})
			continue
		}

		if !IsAppContainer(container) {
			logger.Info("Container not eligible for haloy management", "container_id", containerSummary.ID)
			continue
		}

		labels, err := config.ParseContainerLabels(container.Config.Labels)
		if err != nil {
			logger.Error("Error parsing labels for container", "container_id", containerSummary.ID, "error", err)
			failedContainers = append(failedContainers, FailedContainerInfo{
				ContainerID: containerSummary.ID,
				Error:       err.Error(),
				Labels:      nil, // No labels if parsing failed
			})
			continue
		}

		ip, err := docker.ContainerNetworkIP(container, constants.DockerNetwork)
		if err != nil {
			logger.Error("Error getting IP for container", "container_id", helpers.SafeIDPrefix(container.ID), "error", err)
			failedContainers = append(failedContainers, FailedContainerInfo{
				ContainerID: container.ID,
				Error:       err.Error(),
				Labels:      labels,
			})
			continue
		}

		var port string
		if labels.Port != "" {
			port = labels.Port
		} else {
			port = constants.DefaultContainerPort
		}

		instance := DeploymentInstance{ContainerID: container.ID, IP: ip, Port: port}

		if deployment, exists := newDeployments[labels.AppName]; exists {
			// There is a appName match, check if the deployment ID matches.
			if deployment.Labels.DeploymentID == labels.DeploymentID {
				deployment.Instances = append(deployment.Instances, instance)
				newDeployments[labels.AppName] = deployment
			} else {
				// Replace the deployment if the new one has a higher deployment ID
				if deployment.Labels.DeploymentID < labels.DeploymentID {
					newDeployments[labels.AppName] = Deployment{Labels: labels, Instances: []DeploymentInstance{instance}}
				}
			}
		} else {
			newDeployments[labels.AppName] = Deployment{Labels: labels, Instances: []DeploymentInstance{instance}}
		}
	}

	dm.deploymentsMutex.Lock()
	defer dm.deploymentsMutex.Unlock()

	oldDeployments := dm.deployments
	dm.deployments = newDeployments

	compareResult := compareDeployments(oldDeployments, newDeployments)
	hasChanged = len(compareResult.AddedDeployments) > 0 ||
		len(compareResult.RemovedDeployments) > 0 ||
		len(compareResult.UpdatedDeployments) > 0

	dm.compareResult = compareResult
	return hasChanged, failedContainers, nil
}

func (dm *DeploymentManager) HealthCheckNewContainers(ctx context.Context, logger *slog.Logger) (checked []Deployment, failedContainerIDs []string) {
	for _, deployment := range dm.compareResult.AddedDeployments {
		checked = append(checked, deployment)
	}

	for _, deployment := range dm.compareResult.UpdatedDeployments {
		checked = append(checked, deployment)
	}

	for _, deployment := range checked {
		for _, instance := range deployment.Instances {
			if err := docker.HealthCheckContainer(ctx, dm.cli, logger, instance.ContainerID); err != nil {
				failedContainerIDs = append(failedContainerIDs, instance.ContainerID)
			}
		}
	}
	return checked, failedContainerIDs
}

func (dm *DeploymentManager) Deployments() map[string]Deployment {
	dm.deploymentsMutex.RLock()
	defer dm.deploymentsMutex.RUnlock()

	// Return a copy to prevent external modification after unlock
	deploymentsCopy := make(map[string]Deployment, len(dm.deployments))
	for appName, deployment := range dm.deployments {
		deploymentsCopy[appName] = deployment
	}
	return deploymentsCopy
}

// GetCertificateDomains collects all canonical domains and their aliases for certificate management.
func (dm *DeploymentManager) GetCertificateDomains() []CertificatesDomain {
	dm.deploymentsMutex.RLock()
	defer dm.deploymentsMutex.RUnlock()

	managedDomains := make([]CertificatesDomain, 0, len(dm.deployments)) // Pre-allocate roughly

	for _, deployment := range dm.deployments {
		if deployment.Labels == nil {
			continue // Skip if labels somehow nil
		}
		for _, domain := range deployment.Labels.Domains {
			// Only process if canonical domain is set and not empty
			if domain.Canonical != "" {
				// Ensure Aliases slice is not nil before passing
				aliases := domain.Aliases
				if aliases == nil {
					aliases = []string{}
				}
				managedDomains = append(managedDomains, CertificatesDomain{
					Canonical: domain.Canonical,
					Aliases:   aliases, // Include aliases
					Email:     deployment.Labels.ACMEEmail,
				})
			}
		}
	}
	return managedDomains
}

type compareResult struct {
	UpdatedDeployments map[string]Deployment
	RemovedDeployments map[string]Deployment
	AddedDeployments   map[string]Deployment
}

// compareDeployments analyzes differences between the previous and current deployment states.
// It identifies three types of changes:
// 1. Updated deployments - same app name but different deployment ID or instance configuration
// 2. Removed deployments - deployments that existed before but are no longer present
// 3. Added deployments - new deployments that didn't exist in the previous state
// This comparison is critical for determining when HAProxy configuration should be updated.
func compareDeployments(oldDeployments, newDeployments map[string]Deployment) compareResult {

	updatedDeployments := make(map[string]Deployment)
	removedDeployments := make(map[string]Deployment)
	addedDeployments := make(map[string]Deployment)

	for appName, prevDeployment := range oldDeployments {
		if currentDeployment, exists := newDeployments[appName]; exists {
			if prevDeployment.Labels.DeploymentID != currentDeployment.Labels.DeploymentID {
				updatedDeployments[appName] = currentDeployment
			} else {
				if !instancesEqual(prevDeployment.Instances, currentDeployment.Instances) {
					updatedDeployments[appName] = currentDeployment
				}
			}
		} else {
			removedDeployments[appName] = prevDeployment
		}
	}

	for appName, currentDeployment := range newDeployments {
		if _, exists := oldDeployments[appName]; !exists {
			addedDeployments[appName] = currentDeployment
		}
	}

	result := compareResult{
		UpdatedDeployments: updatedDeployments,
		RemovedDeployments: removedDeployments,
		AddedDeployments:   addedDeployments,
	}

	return result
}

func instancesEqual(a, b []DeploymentInstance) bool {
	if len(a) != len(b) {
		return false
	}

	mapA := make(map[string]bool)
	for _, instance := range a {
		mapA[instance.ContainerID] = true
	}

	for _, instance := range b {
		if !mapA[instance.ContainerID] {
			return false
		}
	}

	return true
}
