package manager

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/ui"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
)

type Updater struct {
	cli               *client.Client
	deploymentManager *DeploymentManager
	certManager       *CertificatesManager
	haproxyManager    *HAProxyManager
}

type UpdaterConfig struct {
	Cli               *client.Client
	DeploymentManager *DeploymentManager
	CertManager       *CertificatesManager
	HAProxyManager    *HAProxyManager
}

func NewUpdater(config UpdaterConfig) *Updater {
	return &Updater{
		cli:               config.Cli,
		deploymentManager: config.DeploymentManager,
		certManager:       config.CertManager,
		haproxyManager:    config.HAProxyManager,
	}
}

type TriggeredByApp struct {
	appName           string
	domains           []config.Domain
	deploymentID      string
	dockerEventAction events.Action // Action that triggered the update (e.g., "start", "stop", etc.)
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

func (u *Updater) Update(ctx context.Context, logger *slog.Logger, reason TriggerReason, app *TriggeredByApp) error {
	// Build Deployments and check if anything has changed (Thread-safe)
	deploymentsHasChanged, failedContainers, err := u.deploymentManager.BuildDeployments(ctx, logger)
	if err != nil {
		return fmt.Errorf("updater: failed to build deployments: %w", err)
	}

	// If we have failed containers, we log them and stop them. We'll do that even if no changes were detected.
	if len(failedContainers) > 0 {
		for _, failedContainer := range failedContainers {
			if failedContainer.Labels != nil {
				logger.Info("Container failed to start - verify configuration and label settings",
					"container_id", helpers.SafeIDPrefix(failedContainer.ContainerID),
					"app", failedContainer.Labels.AppName,
					"deployment_id", failedContainer.Labels.DeploymentID)
			} else {
				logger.Info("Container failed to start - no label info available, check container configuration",
					"container_id", helpers.SafeIDPrefix(failedContainer.ContainerID))
			}

			logger.Info("Stopping container due to error",
				"container_id", helpers.SafeIDPrefix(failedContainer.ContainerID),
				"error", failedContainer.Error)

			err := u.cli.ContainerStop(ctx, failedContainer.ContainerID, container.StopOptions{})
			if err != nil {
				ui.Error(fmt.Sprintf(
					"Critical: Failed to stop container %s. Manual intervention may be required. Check docker logs and container status.",
					helpers.SafeIDPrefix(failedContainer.ContainerID),
				), err)
			} else {
				logger.Info("Stop command issued successfully for container - check logs with docker logs",
					"container_id", helpers.SafeIDPrefix(failedContainer.ContainerID))
			}
		}
	}

	// If no changes were detected, we skip further processing.
	if !deploymentsHasChanged {
		logger.Debug("Updater: No changes detected in deployments, skipping further processing")
		return nil
	}

	checkedDeployments, failedContainerIDs := u.deploymentManager.HealthCheckNewContainers(ctx, logger)
	if len(failedContainerIDs) > 0 {
		return fmt.Errorf("deployment aborted: failed to perform health check on new containers (%s): %w", strings.Join(failedContainerIDs, ", "), err)
	} else {
		apps := make([]string, 0, len(checkedDeployments))
		for _, dep := range checkedDeployments {
			apps = append(apps, dep.Labels.AppName)
		}
		logger.Info("Health check completed", "apps", strings.Join(apps, ", "))
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
		if err := u.certManager.RefreshSync(logger, appCertDomains); err != nil {
			return fmt.Errorf("failed to refresh certificates for app %s: %w", app.appName, err)
		}
	} else {
		u.certManager.Refresh(logger, certDomains)
	}

	// Periodically clean up expired certificates
	if reason == TriggerPeriodicRefresh {
		u.certManager.CleanupExpiredCertificates(logger, certDomains)
	}

	// Get deployments AFTER checking HasChanged
	deployments := u.deploymentManager.Deployments()

	// Apply the HAProxy configuration
	if err := u.haproxyManager.ApplyConfig(ctx, logger, deployments); err != nil {
		return fmt.Errorf("failed to apply HAProxy config for app: %w", err)
	} else {
		logger.Info("HAProxy configuration applied successfully")
	}

	// If an app is provided:
	// - stop old containers, remove and log the result.
	// - log successful deployment for app.

	if app != nil {
		_, err := docker.StopContainers(ctx, u.cli, app.appName, app.deploymentID)
		if err != nil {
			return fmt.Errorf("failed to stop old containers: %w", err)
		}
		_, err = docker.RemoveContainers(ctx, u.cli, app.appName, app.deploymentID)
		if err != nil {
			return fmt.Errorf("failed to remove old containers: %w", err)
		}
	}

	return nil
}
