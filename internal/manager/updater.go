package manager

import (
	"context"
	"fmt"

	"github.com/ameistad/haloy/internal/logging"
)

type Updater struct {
	deploymentManager *DeploymentManager
	certManager       *CertificatesManager
	haproxyManager    *HAProxyManager
}

type UpdaterConfig struct {
	DeploymentManager *DeploymentManager
	CertManager       *CertificatesManager
	HAProxyManager    *HAProxyManager
}

func NewUpdater(config UpdaterConfig) *Updater {
	return &Updater{
		deploymentManager: config.DeploymentManager,
		certManager:       config.CertManager,
		haproxyManager:    config.HAProxyManager,
	}
}

func (u *Updater) Update(ctx context.Context, reason string, logger *logging.Logger) error {

	logger.Debug(fmt.Sprintf("Updater: Starting update process for reason: %s", reason))

	// Build Deployments and check if anything has changed (Thread-safe)
	deploymentsHasChanged, err := u.deploymentManager.BuildDeployments(ctx)
	if err != nil {
		return fmt.Errorf("updater: failed to build deployments (%s): %w", reason, err)
	}
	if !deploymentsHasChanged {
		logger.Debug(fmt.Sprintf("Updater: No changes detected in deployments for reason: %s", reason))
		return nil // Nothing changed, successful exit
	}

	if err := u.deploymentManager.HealthCheckNewContainers(); err != nil {
		return fmt.Errorf("deployment aborted: failed to perform health check on new containers (%s): %w", reason, err)
	}

	logger.Debug(fmt.Sprintf("Updater: Deployment changes detected for reason: %s. Triggering cert and HAProxy updates.", reason))

	// Get domains AFTER checking HasChanged to reflect the latest state
	certDomains := u.deploymentManager.GetCertificateDomains()
	u.certManager.AddDomains(certDomains, logger) // Let CertManager handle duplicates/warnings
	u.certManager.Refresh(logger)                 // Refresh is debounced internally

	// Get deployments AFTER checking HasChanged
	deployments := u.deploymentManager.Deployments() // Gets a safe copy

	// Delegate the entire HAProxy update process (lock, generate, write, signal)
	if err := u.haproxyManager.ApplyConfig(ctx, deployments); err != nil {
		return fmt.Errorf("failed to apply HAProxy config (%s): %w", reason, err)
	} else {
		logger.Info("HAProxy configuration updated successfully")
	}
	return nil
}
