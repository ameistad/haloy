package manager

import (
	"context"
	"fmt"

	"github.com/ameistad/haloy/internal/manager/certificates"
	"github.com/rs/zerolog"
)

type Updater struct {
	deploymentManager *DeploymentManager
	certManager       *certificates.Manager
	haproxyManager    *HAProxyManager
}

type UpdaterConfig struct {
	DeploymentManager *DeploymentManager
	CertManager       *certificates.Manager
	HAProxyManager    *HAProxyManager
}

func NewUpdater(config UpdaterConfig) *Updater {
	return &Updater{
		deploymentManager: config.DeploymentManager,
		certManager:       config.CertManager,
		haproxyManager:    config.HAProxyManager,
	}
}

func (u *Updater) Update(ctx context.Context, reason string, logger zerolog.Logger) error {

	logger.Info().Str("reason", reason).Msg("Updater: Starting deployment update")

	// Build Deployments and check if anything has changed (Thread-safe)
	deploymentsHasChanged, err := u.deploymentManager.BuildDeployments(ctx)
	if err != nil {
		return fmt.Errorf("updater: failed to build deployments (%s): %w", reason, err)
	}
	if !deploymentsHasChanged {
		logger.Info().Str("reason", reason).Msg("Updater: No deployment changes detected. Skipping HAProxy update.")
		return nil // Nothing changed, successful exit
	}

	if err := u.deploymentManager.HealthCheckNewContainers(); err != nil {
		return fmt.Errorf("deployment aborted: failed to perform health check on new containers (%s): %w", reason, err)
	}

	logger.Info().Str("reason", reason).Msg("Deployment changes detected. Triggering cert and HAProxy updates.")

	// Get domains AFTER checking HasChanged to reflect the latest state
	certDomains := u.deploymentManager.GetCertificateDomains()
	u.certManager.AddDomains(certDomains) // Let CertManager handle duplicates/warnings
	u.certManager.Refresh()               // Refresh is debounced internally

	// Get deployments AFTER checking HasChanged
	deployments := u.deploymentManager.Deployments() // Gets a safe copy

	// Delegate the entire HAProxy update process (lock, generate, write, signal)
	if err := u.haproxyManager.ApplyConfig(ctx, deployments); err != nil {
		// Log context with the error
		logger.Error().Err(err).Str("reason", reason).Msg("Updater: Failed to apply HAProxy config")
		return fmt.Errorf("updater: failed to apply HAProxy config (%s): %w", reason, err)
	}

	logger.Info().Str("reason", reason).Msg("Updater: Successfully completed deployment update process")
	return nil
}
