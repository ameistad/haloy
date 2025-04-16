package manager

import (
	"context"
	"fmt"

	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/manager/certificates"
	"github.com/rs/zerolog"
)

type Updater struct {
	deploymentManager *DeploymentManager
	certManager       *certificates.Manager
	haproxyManager    *HAProxyManager
	logger            zerolog.Logger // Base logger, can be overridden with context logger.
}

type UpdaterConfig struct {
	DeploymentManager *DeploymentManager
	CertManager       *certificates.Manager
	HAProxyManager    *HAProxyManager
	Logger            zerolog.Logger
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

	// Retrieve logger from context, falling back to the updater's base logger if none found
	// Note: logging.Ctx returns a pointer, so we dereference it.
	logger := u.getLoggerFromContextOrDefault(ctx)

	logger.Info().Str("reason", reason).Msg("Updater: Starting deployment update")

	// Build Deployments and check if anything has changed (Thread-safe)
	deploymentsHasChanged, err := u.deploymentManager.BuildDeployments(ctx)
	if err != nil {
		// Log context with the error
		logger.Error().Err(err).Str("reason", reason).Msg("Updater: Failed to build deployments")
		return fmt.Errorf("updater: failed to build deployments (%s): %w", reason, err)
	}
	if !deploymentsHasChanged {
		logger.Info().Str("reason", reason).Msg("Updater: No deployment changes detected. Skipping HAProxy update.")
		return nil // Nothing changed, successful exit
	}

	if err := u.deploymentManager.HealthCheckNewContainers(); err != nil {
		return fmt.Errorf("deployment aborted: failed to perform health check on new containers (%s): %w", reason, err)
	}

	logger.Info().Str("reason", reason).Msg("Updater: Deployment changes detected. Triggering cert and HAProxy updates.")

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
		logger.Error().Err(err).Str("reason", reason).Msg("Updater: Failed to apply HAProxy config")
		return fmt.Errorf("updater: failed to apply HAProxy config (%s): %w", reason, err)
	}

	logger.Info().Str("reason", reason).Msg("Updater: Successfully completed deployment update process")
	return nil
}

// Helper to get logger from context or default to the updater's base logger
func (u *Updater) getLoggerFromContextOrDefault(ctx context.Context) zerolog.Logger {
	if logger := logging.GetLoggerFromContext(ctx); logger != nil {
		return *logger // Dereference the pointer returned by GetLoggerFromContext
	}
	return u.logger // Fallback to the base logger stored in the updater instance
}
