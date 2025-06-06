package manager

import (
	"context"
	"fmt"
	"strings"

	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/logging"
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
	AppName             string
	latestDeploymentID  string
	maxContainersToKeep int
	dockerEventAction   events.Action // Action that triggered the update (e.g., "start", "stop", etc.)
}

func (tba *TriggeredByApp) Validate() error {
	if tba.AppName == "" {
		return fmt.Errorf("triggered by app: app name cannot be empty")
	}
	if tba.latestDeploymentID == "" {
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
	deploymentsHasChanged, err := u.deploymentManager.BuildDeployments(ctx)
	if err != nil {
		return fmt.Errorf("updater: failed to build deployments: %w", err)
	}
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

	// Get domains AFTER checking HasChanged to reflect the latest state
	certDomains := u.deploymentManager.GetCertificateDomains()
	u.certManager.AddDomains(certDomains, logger)

	// If an app is provided we refresh the certs synchronously so we can log the result.
	// Otherwise, we refresh them asynchronously to avoid blocking the main update process.
	if app != nil {
		u.certManager.RefreshSync(logger)
	} else {
		u.certManager.Refresh(logger)
	}

	// Get deployments AFTER checking HasChanged
	deployments := u.deploymentManager.Deployments() // Gets a safe copy

	// Delegate the entire HAProxy update process (lock, generate, write, signal)
	if err := u.haproxyManager.ApplyConfig(ctx, deployments); err != nil {
		return fmt.Errorf("failed to apply HAProxy config for app: %w", err)
	} else {
		logger.Info("HAProxy configuration updated successfully")
	}

	// If an app is provided, stop and remove old containers
	if app != nil {
		stoppedIDs, err := docker.StopContainers(ctx, u.dockerClient, app.AppName, app.latestDeploymentID)
		if err != nil {
			return fmt.Errorf("failed to stop old containers: %w", err)
		}
		removeContainersParams := docker.RemoveContainersParams{
			Context:             ctx,
			DockerClient:        u.dockerClient,
			AppName:             app.AppName,
			IgnoreDeploymentID:  app.latestDeploymentID,
			MaxContainersToKeep: app.maxContainersToKeep,
		}
		removedContainers, err := docker.RemoveContainers(removeContainersParams)
		if err != nil {
			return fmt.Errorf("failed to remove old containers: %w", err)
		}
		logger.Info(fmt.Sprintf("Stopped %d container(s) and removed %d old container(s)", len(stoppedIDs), len(removedContainers)))
		logger.Info(fmt.Sprintf("🎉 Successfully deployed %s with deployment ID %s", app.AppName, app.latestDeploymentID))
	}

	return nil
}
