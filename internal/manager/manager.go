package manager

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ameistad/haloy/internal/api"
	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/constants"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/storage"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

const (
	maintenanceInterval = 12 * time.Hour  // Interval for periodic maintenance tasks
	eventDebounceDelay  = 3 * time.Second // Delay for debouncing container events
	updateTimeout       = 2 * time.Minute // Max time for a single update operation
)

type ContainerEvent struct {
	Event     events.Message
	Container container.InspectResponse
	Labels    *config.ContainerLabels
}

func RunManager(debug bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}

	// Use a log broker to allow streaming logs to the API server
	logBroker := logging.NewLogBroker()
	logger := logging.NewLogger(logLevel, logBroker)

	logger.Info("Haloy manager started",
		"version", constants.Version,
		"network", constants.DockerNetwork,
		"debug", debug)

	if debug {
		logger.Info("Debug mode enabled: No changes will be applied to HAProxy. Staging certificates will be used for all domains.")
	}

	db, err := storage.New()
	if err != nil {
		logger.Error("Failed to initialize database", "error", err)
		return
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		logger.Error("Failed to run database migrations", "error", err)
		return
	}
	logger.Info("Database initialized successfully")

	dataDir, err := config.DataDir()
	if err != nil {
		logger.Error("Failed to get data directory", "error", err)
		return
	}
	configDir, err := config.ConfigDir()
	if err != nil {
		logger.Error("Failed to get manager config directory", "error", err)
		return
	}
	configFilePath := filepath.Join(configDir, constants.ManagerConfigFileName)
	managerConfig, err := config.LoadManagerConfig(configFilePath)
	if err != nil {
		logger.Error("Failed to load configuration file", "error", err)
		return
	}

	cli, err := docker.NewClient(ctx)
	if err != nil {
		logging.LogFatal(logger, "Failed to create Docker client", "error", err)
	}
	defer cli.Close()

	apiToken := os.Getenv(constants.EnvVarAPIToken)
	if apiToken == "" {
		logging.LogFatal(logger, "%s environment variable not set", constants.EnvVarAPIToken)
	}

	apiServer := api.NewServer(apiToken, logBroker, logLevel)
	go func() {
		logger.Info(fmt.Sprintf("Starting API server on :%s...", constants.APIServerPort))
		if err := apiServer.ListenAndServe(fmt.Sprintf(":%s", constants.APIServerPort)); err != nil && err != http.ErrServerClosed {
			logging.LogFatal(logger, "API server failed", "error", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Channel for Docker events
	eventsChan := make(chan ContainerEvent)
	errorsChan := make(chan error)

	// Channel for signaling cert updates needing HAProxy reload
	certUpdateSignal := make(chan string, 5)

	deploymentManager := NewDeploymentManager(cli, managerConfig)
	certManagerConfig := CertificatesManagerConfig{
		CertDir:          filepath.Join(dataDir, constants.CertStorageDir),
		HTTPProviderPort: constants.CertificatesHTTPProviderPort,
		TlsStaging:       debug,
	}
	certManager, err := NewCertificatesManager(certManagerConfig, certUpdateSignal)
	if err != nil {
		logging.LogFatal(logger, "Failed to create certificate manager", "error", err)
	}
	haproxyManager := NewHAProxyManager(cli, managerConfig, filepath.Join(dataDir, constants.HAProxyConfigDir), debug)
	updaterConfig := UpdaterConfig{
		Cli:               cli,
		DeploymentManager: deploymentManager,
		CertManager:       certManager,
		HAProxyManager:    haproxyManager,
	}

	updater := NewUpdater(updaterConfig)
	if err := updater.Update(ctx, logger, TriggerReasonInitial, nil); err != nil {
		logger.Error("Initial update failed", "error", err)
	}

	logger.Info("Haloy manager initialized",
		logging.AttrManagerInitComplete, true, // signal that the initialization is complete
	)

	// To prevent multiple updates in quick succession
	debouncer := helpers.NewDebouncer(eventDebounceDelay)

	// Docker event listener
	go listenForDockerEvents(ctx, cli, eventsChan, errorsChan, logger)

	maintenanceTicker := time.NewTicker(maintenanceInterval)
	defer maintenanceTicker.Stop()

	// Use the latest event when we debounce.
	latestDeploymentID := make(map[string]string)
	latestEventAction := make(map[string]events.Action)
	latestDomains := make(map[string][]config.Domain)
	lastProcessedDeployment := make(map[string]string) // Avoid processing the same deployment multiple times

	// Main event loop
	for {
		select {
		case <-sigChan:
			logger.Info("Received shutdown signal, stopping manager...")
			if certManager != nil {
				certManager.Stop()
			}
			cancel()
			return
		case e := <-eventsChan:
			appName := e.Labels.AppName // The app name that triggered the event.
			deploymentID := e.Labels.DeploymentID
			eventAction := e.Event.Action
			if deploymentID >= latestDeploymentID[appName] {
				latestDeploymentID[appName] = deploymentID
				latestEventAction[appName] = eventAction
				latestDomains[appName] = e.Labels.Domains
			}

			updateAction := func() {
				currentDeploymentID := latestDeploymentID[appName]
				currentDomains := latestDomains[appName]
				// Skip if we've already processed this deployment successfully
				if lastProcessedDeployment[appName] == currentDeploymentID {
					logger.Debug("Skipping already processed deployment",
						"app", appName,
						"deploymentID", currentDeploymentID,
						"eventAction", latestEventAction[appName])
					return
				}

				// Create a deployment-specific logger that will stream to SSE clients
				deploymentLogger := logging.NewDeploymentLogger(currentDeploymentID, logLevel, logBroker)

				// New context with timeout
				updateCtx, cancelUpdate := context.WithTimeout(ctx, updateTimeout)
				defer cancelUpdate()

				app := &TriggeredByApp{
					appName:           appName,
					domains:           currentDomains,
					deploymentID:      currentDeploymentID,
					dockerEventAction: latestEventAction[appName],
				}

				if err := app.Validate(); err != nil {
					deploymentLogger.Error("App data not valid", "error", err)
					return
				}
				if err := updater.Update(updateCtx, deploymentLogger, TriggerReasonAppUpdated, app); err != nil {
					logging.LogDeploymentFailed(deploymentLogger, currentDeploymentID, appName,
						"Deployment failed", err)
					return
				}

				if latestEventAction[appName] == events.ActionStart {
					lastProcessedDeployment[appName] = currentDeploymentID
					canonicalDomains := make([]string, len(currentDomains))
					for i, domain := range currentDomains {
						canonicalDomains[i] = domain.Canonical
					}
					logging.LogDeploymentComplete(deploymentLogger, canonicalDomains, currentDeploymentID, appName,
						fmt.Sprintf("Successfully deployed %s", appName))
				}
			}

			debouncer.Debounce(appName, updateAction)

		case domainUpdated := <-certUpdateSignal:
			logger.Info("Received cert update signal", "domain", domainUpdated)

			go func() {
				// Use a timeout context for this specific task
				updateCtx, cancelUpdate := context.WithTimeout(ctx, 60*time.Second)
				defer cancelUpdate()

				u := updater
				// Update only needs to apply config, not full build/check
				// We assume the deployment state triggering the cert update is still valid.
				currentDeployments := u.deploymentManager.Deployments()
				if err := u.haproxyManager.ApplyConfig(updateCtx, logger, currentDeployments); err != nil {
					logger.Error("Background HAProxy update failed",
						"reason", "cert update",
						"domain", domainUpdated,
						"error", err)
				}
			}()

		case err := <-errorsChan:
			logger.Error("Error from docker events", "error", err)
		case <-maintenanceTicker.C:
			logger.Info("Performing periodic maintenance...")
			_, err := docker.PruneImages(ctx, cli, logger)
			if err != nil {
				logger.Warn("Failed to prune images", "error", err)
			}
			go func() {
				deploymentCtx, cancelDeployment := context.WithCancel(ctx)
				defer cancelDeployment()

				// Perform a background update to ensure the system is in sync
				// This will check the running containers and update HAProxy and renew and cleanup certificates if needed.
				u := updater
				if err := u.Update(deploymentCtx, logger, TriggerPeriodicRefresh, nil); err != nil {
					logger.Error("Background update failed", "error", err)
				}
			}()
		}
	}
}

// listenForDockerEvents sets up a listener for Docker events
func listenForDockerEvents(ctx context.Context, cli *client.Client, eventsChan chan ContainerEvent, errorsChan chan error, logger *slog.Logger) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "container")

	// Define allowed actions for event processing
	allowedActions := map[string]struct{}{
		"start":   {},
		"restart": {},
		"die":     {},
		"stop":    {},
		"kill":    {},
	}

	eventOptions := events.ListOptions{
		Filters: filterArgs,
	}

	events, errs := cli.Events(ctx, eventOptions)

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			if _, ok := allowedActions[string(event.Action)]; ok {
				container, err := cli.ContainerInspect(ctx, event.Actor.ID)
				if err != nil {
					logger.Error("Error inspecting container",
						"containerID", helpers.SafeIDPrefix(event.Actor.ID),
						"error", err)
					continue
				}
				eligible := IsAppContainer(container)

				// We'll only process events for containers that have been marked with haloy labels.
				if eligible {
					labels, err := config.ParseContainerLabels(container.Config.Labels)
					if err != nil {
						logger.Error("Error parsing container labels", "error", err)
						return
					}

					logger.Debug("Container is eligible",
						"event", string(event.Action),
						"containerID", helpers.SafeIDPrefix(event.Actor.ID),
						"deploymentID", labels.DeploymentID)

					containerEvent := ContainerEvent{
						Event:     event,
						Container: container,
						Labels:    labels,
					}
					eventsChan <- containerEvent
				} else {
					logger.Debug("Container not eligible for haloy management",
						"containerID", helpers.SafeIDPrefix(event.Actor.ID))
				}
			}
		case err := <-errs:
			if err != nil {
				errorsChan <- err
				// For non-fatal errors we'll try to reconnect instead of exiting
				if err != io.EOF && !strings.Contains(err.Error(), "connection refused") {
					// Attempt to reconnect
					time.Sleep(5 * time.Second)
					events, errs = cli.Events(ctx, eventOptions)
					continue
				}
			}
			return
		}
	}
}

// IsAppContainer checks if a container should be handled by haloy.
func IsAppContainer(container container.InspectResponse) bool {

	if container.Config.Labels[config.LabelRole] != config.AppLabelRole {
		return false
	}

	isOnNetwork := isOnNetworkCheck(container, constants.DockerNetwork)
	return isOnNetwork
}

func isOnNetworkCheck(container container.InspectResponse, networkName string) bool {
	for netName := range container.NetworkSettings.Networks {
		if netName == networkName {
			return true
		}
	}
	return false
}
