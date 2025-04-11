package manager

import (
	"context"
	"fmt"

	"github.com/ameistad/haloy/internal/manager/certificates"
	"github.com/sirupsen/logrus"
)

type Updater struct {
	deploymentManager *DeploymentManager
	certManager       *certificates.Manager
	haproxyManager    *HAProxyManager
	logger            *logrus.Logger
}

type UpdaterConfig struct {
	DeploymentManager *DeploymentManager
	CertManager       *certificates.Manager
	HAProxyManager    *HAProxyManager
	Logger            *logrus.Logger
}

func NewUpdater(config UpdaterConfig) *Updater {
	return &Updater{
		deploymentManager: config.DeploymentManager,
		certManager:       config.CertManager,
		haproxyManager:    config.HAProxyManager,
		logger:            config.Logger,
	}
}

func (u *Updater) Update(ctx context.Context, reason string) error {
	u.logger.Infof("Updater: Starting deployment update (%s)", reason)

	// Build Deployments and check if anything has changed (Thread-safe)
	deploymentsHasChanged, err := u.deploymentManager.BuildDeployments(ctx)
	if err != nil {
		// Log context with the error
		u.logger.Errorf("Updater: Failed to build deployments (%s): %v", reason, err)
		return fmt.Errorf("updater: failed to build deployments (%s): %w", reason, err)
	}
	if !deploymentsHasChanged {
		u.logger.Infof("Updater: No deployment changes detected (%s). Skipping HAProxy update.", reason)
		return nil // Nothing changed, successful exit
	}

	if err := u.deploymentManager.HealthCheckNewContainers(); err != nil {
		return fmt.Errorf("deployment aborted: failed to perform health check on new containers (%s): %w", reason, err)
	}

	u.logger.Infof("Updater: Deployment changes detected (%s). Triggering cert and HAProxy updates.", reason)

	// --- Certificate Update ---
	// Get domains AFTER checking HasChanged to reflect the latest state
	certDomains := u.deploymentManager.GetCertificateDomains()
	u.certManager.AddDomains(certDomains) // Let CertManager handle duplicates/warnings
	u.certManager.Refresh()               // Refresh is debounced internally

	// --- HAProxy Update ---
	// Get deployments AFTER checking HasChanged
	deployments := u.deploymentManager.Deployments() // Gets a safe copy

	// Delegate the entire HAProxy update process (lock, generate, write, signal)
	if err := u.haproxyManager.ApplyConfig(ctx, deployments); err != nil {
		// Log context with the error
		u.logger.Errorf("Updater: Failed to apply HAProxy config (%s): %v", reason, err)
		return fmt.Errorf("updater: failed to apply HAProxy config (%s): %w", reason, err)
	}

	u.logger.Infof("Updater: Successfully completed deployment update process (%s)", reason)
	return nil
}
