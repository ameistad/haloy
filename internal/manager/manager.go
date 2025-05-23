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
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	RefreshInterval    = 30 * time.Minute
	HAProxyConfigDir   = "/haproxy-config"
	CertificatesDir    = "/cert-storage"
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
	logLevel := zerolog.InfoLevel
	if dryRun {
		logLevel = zerolog.DebugLevel
	}

	// Initialize logging (configures global logger and starts server)
	logServer, err := logging.Init(ctx, logLevel) // Pass address
	if err != nil {
		fmt.Fprintf(os.Stderr, "CRITICAL: Failed to initialize logging: %v", err)
	}

	// Ensure server is stopped on exit if it was started
	if logServer != nil {
		defer logServer.Stop()
	}
	if dryRun {
		log.Debug().Msg("Debug logging enabled")
	}

	// Initialize Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create Docker client")
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
	deploymentManager := NewDeploymentManager(ctx, dockerClient)

	// Create and start the certifications manager
	certManagerConfig := CertificatesManagerConfig{
		CertDir:          CertificatesDir,
		HTTPProviderPort: HTTPProviderPort,
		TlsStaging:       dryRun,
	}
	certManager, err := NewCertificatesManager(certManagerConfig, certUpdateSignal)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create certificate manager")
		return
	}

	// Create the HAProxy manager
	haproxyManagerConfig := HAProxyManagerConfig{
		DockerClient: dockerClient,
		Logger:       log.Logger,
		ConfigDir:    HAProxyConfigDir,
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
	updateLogger := log.With().Str("reason", updateReason).Logger()
	if err := updater.Update(ctx, updateReason, updateLogger); err != nil {
		log.Error().Err(err).Str("reason", updateReason).Msg("Background update failed")
	}

	// Create a debouncer to prevent multiple updates in quick succession
	debouncer := helpers.NewDebouncer(EventDebounceDelay)

	// Start Docker event listener
	go listenForDockerEvents(ctx, dockerClient, eventsChan, errorsChan)

	// Start periodic full refresh
	refreshTicker := time.NewTicker(RefreshInterval)
	defer refreshTicker.Stop()

	log.Info().Msgf("Haloy manager version %s started on network %s...", version.Version, config.DockerNetwork)

	// Main event loop
	for {
		select {
		case <-sigChan:
			log.Ctx(ctx).Info().Msg("Received shutdown signal, stopping manager...")
			if certManager != nil {
				certManager.Stop()
			}
			cancel()
			return
		case e := <-eventsChan:
			appName := e.Labels.AppName
			reason := fmt.Sprintf("container %s: %s", e.Event.Action, appName)
			log.Debug().Str("reason", reason).Str("appName", appName).Msg("Received event, debouncing update")

			// Define the action to be executed after debouncing
			updateAction := func() {
				// Create a contextual logger for this specific debounced update.
				appLogger := log.With().Str("appName", appName).Str("trigger", "debounced_event").Logger()
				appLogger.Debug().Str("reason", reason).Msg("Executing debounced update")

				// Create a context with a timeout for this specific update task.
				// Use the main manager context `ctx` as the parent.
				updateCtx, cancelUpdate := context.WithTimeout(ctx, UpdateTimeout)
				defer cancelUpdate()

				// Run the actual update using the captured updater.
				// Pass the specific reason and logger.
				if err := updater.Update(updateCtx, reason, appLogger); err != nil {
					appLogger.Error().Err(err).Str("reason", reason).Msg("Debounced HAProxy configuration update failed")
				} else {
					appLogger.Info().Str("reason", reason).Msg("Debounced HAProxy configuration update successful")
				}
			}

			// Use the generic debouncer, keyed by appName
			debouncer.Debounce(appName, updateAction)

		case domainUpdated := <-certUpdateSignal:
			reason := fmt.Sprintf("post-certificate update for %s", domainUpdated)
			log.Debug().Str("reason", reason).Msg("Received cert update signal")
			// Launch a background HAProxy update
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
					log.Error().Err(err).Str("reason", updateReason).Msg("Background HAProxy update failed")
				}
			}(reason)

		case err := <-errorsChan:
			log.Error().Err(err).Msg("Error from Docker events")
		case <-refreshTicker.C:
			reason := "periodic full refresh"
			log.Info().Str("reason", reason).Msg("Received event")

			go func(updateReason string) {
				deploymentCtx, cancelDeployment := context.WithCancel(ctx)
				defer cancelDeployment()

				u := updater // Capture updater
				updateLogger := log.With().Str("reason", updateReason).Logger()
				if err := u.Update(deploymentCtx, updateReason, updateLogger); err != nil {
					log.Error().Err(err).Str("reason", updateReason).Msg("Background update failed")
				}
			}(reason)
		}
	}
}

// listenForDockerEvents sets up a listener for Docker events
func listenForDockerEvents(ctx context.Context, dockerClient *client.Client, eventsChan chan ContainerEvent, errorsChan chan error) {
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
			// Only process events for containers on our network
			if event.Action == "start" || event.Action == "die" || event.Action == "stop" || event.Action == "kill" {

				container, err := dockerClient.ContainerInspect(ctx, event.Actor.ID)
				if err != nil {
					log.Error().Err(err).Str("container", helpers.SafeIDPrefix(event.Actor.ID)).Msg("Error inspecting container")
					continue
				}
				eligible := IsAppContainer(container)

				// We'll only process events for containers that have been marked with haloy labels.
				if eligible {
					labels, err := config.ParseContainerLabels(container.Config.Labels)
					if err != nil {
						log.Error().Err(err).Msg("Error parsing container labels")
						return
					}

					containerEvent := ContainerEvent{
						Event:     event,
						Container: container,
						Labels:    labels,
					}
					eventsChan <- containerEvent
				} else {
					log.Info().Msgf("Container %s is not eligible for haloy management", helpers.SafeIDPrefix(event.Actor.ID))
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
