package manager

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/helpers"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/version"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

const (
	RefreshInterval    = 30 * time.Minute
	HAProxyConfigPath  = "/haproxy-config"
	CertificatesPath   = "/cert-storage"
	LogsPath           = "/logs"
	HTTPProviderPort   = "8080"
	EventDebounceDelay = 1 * time.Second // Delay for debouncing container events
	UpdateTimeout      = 2 * time.Minute // Max time for a single update operation
)

type ContainerEvent struct {
	Event     events.Message
	Container container.InspectResponse
	Labels    *config.ContainerLabels
}

func RunManager(dryRun bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize logging
	logLevel := logging.INFO
	if dryRun {
		logLevel = logging.DEBUG
	}
	logger, err := logging.NewLogger(logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CRITICAL: Failed to initialize logging: %v", err)
	}

	logger.Info(fmt.Sprintf("Haloy manager version %s started on network %s...", version.Version, config.DockerNetwork))

	if dryRun {
		logger.Info("Running in dry-run mode, no changes will be applied to HAProxy")
		logger.Debug("Debug logging enabled")
	}

	// Initialize Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Fatal("Failed to create Docker client", err)
	}
	defer dockerClient.Close()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Channel for Docker events
	eventsChan := make(chan ContainerEvent)
	errorsChan := make(chan error)

	// Channel for signaling cert updates needing HAProxy reload
	// Buffered channel to prevent blocking CertManager if RunManager is busy
	certUpdateSignal := make(chan string, 5)

	// Create deployment manager
	deploymentManager := NewDeploymentManager(ctx, dockerClient, logger)

	// Create and start the certifications manager
	certManagerConfig := CertificatesManagerConfig{
		CertDir:          CertificatesPath,
		HTTPProviderPort: HTTPProviderPort,
		TlsStaging:       dryRun,
	}
	certManager, err := NewCertificatesManager(certManagerConfig, certUpdateSignal)
	if err != nil {
		logger.Fatal("Failed to create certificate manager", err)
		return
	}

	// Create the HAProxy manager
	haproxyManagerConfig := HAProxyManagerConfig{
		DockerClient: dockerClient,
		Logger:       logger,
		ConfigDir:    HAProxyConfigPath,
		DryRun:       dryRun,
	}
	haproxyManager := NewHAProxyManager(haproxyManagerConfig)

	// Updater to glue deployment manager and certificate manager and handle HAProxy updates.
	updaterConfig := UpdaterConfig{
		DeploymentManager: deploymentManager,
		CertManager:       certManager,
		HAProxyManager:    haproxyManager,
	}

	// Perform initial update
	updater := NewUpdater(updaterConfig)
	updateReason := "initial update"
	if err := updater.Update(ctx, updateReason, logger); err != nil {
		logger.Error("Background update failed", err)
	}

	// Create a debouncer to prevent multiple updates in quick succession
	debouncer := helpers.NewDebouncer(EventDebounceDelay)

	// Start Docker event listener
	go listenForDockerEvents(ctx, dockerClient, eventsChan, errorsChan, logger)

	// Start periodic full refresh
	refreshTicker := time.NewTicker(RefreshInterval)
	defer refreshTicker.Stop()

	// Track the latest deploymentID per appName. This way we use the latest ID for logging.
	latestDeploymentID := make(map[string]string)

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
			appName := e.Labels.AppName
			deploymentID := e.Labels.DeploymentID
			reason := fmt.Sprintf("container %s: %s", e.Event.Action, appName)
			if deploymentID > latestDeploymentID[appName] {
				latestDeploymentID[appName] = deploymentID
			}

			fmt.Printf("%s: Manager event for deployment ID: %s\n", appName, e.Labels.DeploymentID)

			updateAction := func() {
				// Create a logger for this specific deployment id. This will write a log file for the specific deployment ID.
				deploymentLogger, err := logging.NewLogger(logger.Level)
				if err != nil {
					logger.Error("Failed to create logger for deployment update", err)
					return
				}
				id := latestDeploymentID[appName]
				if id != "" {
					deploymentLogger.SetDeploymentIDFileWriter(LogsPath, id)
				}
				// Create a context with a timeout for this specific update task.
				// Use the main manager context `ctx` as the parent.
				updateCtx, cancelUpdate := context.WithTimeout(ctx, UpdateTimeout)
				defer cancelUpdate()
				defer deploymentLogger.CloseLog()
				if err := updater.Update(updateCtx, reason, deploymentLogger); err != nil {
					deploymentLogger.Error("Debounced HAProxy configuration update failed", err)
				}
			}

			debouncer.Debounce(appName, updateAction)

		case domainUpdated := <-certUpdateSignal:
			reason := fmt.Sprintf("post-certificate update for %s", domainUpdated)
			logger.Debug(fmt.Sprintf("Received cert update signal: %s", reason))

			go func(updateReason string) {
				// Use a timeout context for this specific task
				updateCtx, cancelUpdate := context.WithTimeout(ctx, 60*time.Second)
				defer cancelUpdate()

				u := updater // Capture updater
				// Important: Update only needs to apply config, not full build/check
				// We assume the deployment state triggering the cert update is still valid.
				// Get the current deployment state
				currentDeployments := u.deploymentManager.Deployments()
				// Directly apply HAProxy config
				if err := u.haproxyManager.ApplyConfig(updateCtx, currentDeployments); err != nil {
					logger.Error(fmt.Sprintf("Background HAProxy update failed for reason: %s", updateReason), err)
				}
			}(reason)

		case err := <-errorsChan:
			logger.Error("Error from docker events", err)
		case <-refreshTicker.C:
			reason := "periodic full refresh"
			logger.Info("Performing Periodic full refresh")

			go func(updateReason string) {
				deploymentCtx, cancelDeployment := context.WithCancel(ctx)
				defer cancelDeployment()

				u := updater // Capture updater
				if err := u.Update(deploymentCtx, updateReason, logger); err != nil {
					logger.Error("Background update failed", err)
				}
			}(reason)
		}
	}
}

// listenForDockerEvents sets up a listener for Docker events
func listenForDockerEvents(ctx context.Context, dockerClient *client.Client, eventsChan chan ContainerEvent, errorsChan chan error, logger *logging.Logger) {
	// Set up filter for container events
	filterArgs := filters.NewArgs()
	filterArgs.Add("type", "container")

	// Start listening for events
	eventOptions := events.ListOptions{
		Filters: filterArgs,
	}

	events, errs := dockerClient.Events(ctx, eventOptions)

	// Forward events and errors to our channels
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-events:
			if event.Action == "start" || event.Action == "die" || event.Action == "stop" || event.Action == "kill" {
				container, err := dockerClient.ContainerInspect(ctx, event.Actor.ID)
				if err != nil {
					logger.Error(fmt.Sprintf("Error inspecting container id %s", helpers.SafeIDPrefix(event.Actor.ID)), err)
					continue
				}
				eligible := IsAppContainer(container)

				// We'll only process events for containers that have been marked with haloy labels.
				if eligible {
					labels, err := config.ParseContainerLabels(container.Config.Labels)
					if err != nil {
						logger.Error("Error parsing container labels", err)
						return
					}

					containerEvent := ContainerEvent{
						Event:     event,
						Container: container,
						Labels:    labels,
					}
					eventsChan <- containerEvent
				} else {
					logger.Info(fmt.Sprintf("Container %s is not eligible for haloy management", helpers.SafeIDPrefix(event.Actor.ID)))
				}
			}
		case err := <-errs:
			if err != nil {
				errorsChan <- err
				// For non-fatal errors we'll try to reconnect instead of exiting
				if err != io.EOF && !strings.Contains(err.Error(), "connection refused") {
					// Attempt to reconnect
					time.Sleep(5 * time.Second)
					events, errs = dockerClient.Events(ctx, eventOptions)
					continue
				}
			}
			return
		}
	}
}

// IsAppContainer checks if a container should be handled by haloy.
func IsAppContainer(container container.InspectResponse) bool {

	// Check if the container has the correct labels.
	if container.Config.Labels[config.LabelRole] != config.AppLabelRole {
		return false
	}

	isOnNetwork := isOnNetworkCheck(container, config.DockerNetwork)
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
