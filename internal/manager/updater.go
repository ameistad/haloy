package manager

import (
	"context"
	"fmt"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
)

type Updater struct {
	dockerClient      *client.Client
	deploymentManager *DeploymentManager
	certManager       *CertificatesManager
	haproxyManager    *HAProxyManager
}

type UpdaterConfig struct {
	DockerClient      *client.Client
	DeploymentManager *DeploymentManager
	CertManager       *CertificatesManager
	HAProxyManager    *HAProxyManager
}

func NewUpdater(config UpdaterConfig) *Updater {
	return &Updater{
		dockerClient:      config.DockerClient,
		deploymentManager: config.DeploymentManager,
		certManager:       config.CertManager,
		haproxyManager:    config.HAProxyManager,
	}
}

type TriggeredByApp struct {
	appName             string
	domains             []config.Domain
	deploymentID        string
	maxContainersToKeep int
	dockerEventAction   events.Action // Action that triggered the update (e.g., "start", "stop", etc.)
}

func (tba *TriggeredByApp) Validate() error {
	if tba.appName == "" {
		return fmt.Errorf("triggered by app: app name cannot be empty")
	}
	if len(tba.domains) == 0 {
		return fmt.Errorf("triggered by app: domains cannot be empty")
	}
	for i, domain := range tba.domains {
		if domain.Canonical == "" {
			return fmt.Errorf("triggered by app: Canonical name cannot be empty in index %d", i)
		}
	}
	if tba.deploymentID == "" {
		return fmt.Errorf("triggered by app: latest deployment ID cannot be empty")
	}
	if tba.maxContainersToKeep < 0 {
		return fmt.Errorf("triggered by app: max containers to keep must be non-negative")
	}
	if tba.dockerEventAction == "" {
		return fmt.Errorf("triggered by app: docker event action cannot be empty")
	}
	return nil
}

type TriggerReason int

const (
	TriggerReasonInitial    TriggerReason = iota // Initial update at startup
	TriggerReasonAppUpdated                      // An app container was stopped, killed or removed
	TriggerPeriodicRefresh                       // Periodic refresh (e.g., every 5 minutes)
)

func (r TriggerReason) String() string {
	switch r {
	case TriggerReasonInitial:
		return "initial update"
	case TriggerReasonAppUpdated:
		return "app updated"
	case TriggerPeriodicRefresh:
		return "periodic refresh"
	default:
		return "unknown"
	}
}

func (u *Updater) Update(ctx context.Context, logger *logging.Logger, reason TriggerReason, app *TriggeredByApp) error {
	// Build Deployments and check if anything has changed (Thread-safe)
	deploymentsHasChanged, failedContainers, err := u.deploymentManager.BuildDeployments(ctx)
	if err != nil {
		return fmt.Errorf("updater: failed to build deployments: %w", err)
	}

	// If we have failed containers, we log them and stop them. We'll do that even if no changes were detected.
	if len(failedContainers) > 0 {
		for _, failedContainer := range failedContainers {
			if failedContainer.Labels != nil {
				logger.Info(fmt.Sprintf(
					"Error: Container %s [App: %s, DeploymentID: %s] failed to start. Verify the container's configuration and label settings.",
					helpers.SafeIDPrefix(failedContainer.ContainerID),
					failedContainer.Labels.AppName,
					failedContainer.Labels.DeploymentID,
				))
			} else {
				logger.Info(fmt.Sprintf(
					"Error: Container %s failed to start and no label info is available. Please check the container configuration.",
					helpers.SafeIDPrefix(failedContainer.ContainerID),
				))
			}

			logger.Info(fmt.Sprintf(
				"Attempting to stop container %s due to error: %s. This error could be caused by network issues or misconfiguration. Check if the container is attached to the correct network.",
				helpers.SafeIDPrefix(failedContainer.ContainerID),
				failedContainer.Error,
			))

			err := u.dockerClient.ContainerStop(ctx, failedContainer.ContainerID, container.StopOptions{})
			if err != nil {
				ui.Error(fmt.Sprintf(
					"Critical: Failed to stop container %s. Manual intervention may be required. Check docker logs and container status.",
					helpers.SafeIDPrefix(failedContainer.ContainerID),
				), err)
			} else {
				logger.Info(fmt.Sprintf(
					"Stop command issued successfully for container %s. For further details, review logs with: docker logs %s",
					helpers.SafeIDPrefix(failedContainer.ContainerID),
					helpers.SafeIDPrefix(failedContainer.ContainerID),
				))
			}
		}
	}

	// If no changes were detected, we skip further processing.
	if !deploymentsHasChanged {
		logger.Debug("Updater: No changes detected in deployments, skipping further processing")
		return nil
	}

	checkedDeployments, failedContainerIDs := u.deploymentManager.HealthCheckNewContainers()
	if len(failedContainerIDs) > 0 {
		return fmt.Errorf("deployment aborted: failed to perform health check on new containers (%s): %w", strings.Join(failedContainerIDs, ", "), err)
	} else {
		apps := make([]string, 0, len(checkedDeployments))
		for _, dep := range checkedDeployments {
			apps = append(apps, dep.Labels.AppName)
		}
		logger.Info(fmt.Sprintf("Health check completed for %s", strings.Join(apps, ", ")))
	}

	certDomains := u.deploymentManager.GetCertificateDomains()

	// If an app is provided we refresh the certs synchronously so we can log the result.
	// Otherwise, we refresh them asynchronously to avoid blocking the main update process.
	// We also refresh the certs for that app only.
	if app != nil && len(app.domains) > 0 {
		appCanonicalDomains := make(map[string]struct{}, len(app.domains))
		for _, domain := range app.domains {
			appCanonicalDomains[domain.Canonical] = struct{}{}
		}

		var appCertDomains []CertificatesDomain
		for _, certDomain := range certDomains {
			if _, ok := appCanonicalDomains[certDomain.Canonical]; ok {
				appCertDomains = append(appCertDomains, certDomain)
			}
		}
		u.certManager.RefreshSync(logger, appCertDomains)
	} else {
		u.certManager.Refresh(logger, certDomains)
	}

	// Periodically clean up expired certificates
	if reason == TriggerPeriodicRefresh {
		u.certManager.CleanupExpiredCertificates(logger, certDomains)
	}

	// Get deployments AFTER checking HasChanged
	deployments := u.deploymentManager.Deployments() // Gets a safe copy

	// Delegate the entire HAProxy update process (lock, generate, write, signal)
	if err := u.haproxyManager.ApplyConfig(ctx, deployments); err != nil {
		return fmt.Errorf("failed to apply HAProxy config for app: %w", err)
	} else {
		logger.Info("HAProxy configuration updated successfully")
	}

	// If an app is provided:
	// - stop old containers, remove and log the result.
	// - log successful deployment for app.

	if app != nil {
		stoppedIDs, err := docker.StopContainers(ctx, u.dockerClient, app.appName, app.deploymentID)
		if err != nil {
			return fmt.Errorf("failed to stop old containers: %w", err)
		}
		removeContainersParams := docker.RemoveContainersParams{
			Context:             ctx,
			DockerClient:        u.dockerClient,
			AppName:             app.appName,
			IgnoreDeploymentID:  app.deploymentID,
			MaxContainersToKeep: app.maxContainersToKeep,
		}
		removedContainers, err := docker.RemoveContainers(removeContainersParams)
		if err != nil {
			return fmt.Errorf("failed to remove old containers: %w", err)
		}
		logger.Info(fmt.Sprintf("Stopped %d container(s) and removed %d old container(s)", len(stoppedIDs), len(removedContainers)))
		logger.Success(fmt.Sprintf("Successfully deployed %s with deployment ID %s", app.appName, app.deploymentID))
	}

	return nil
}
