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
	"github.com/ameistad/haloy/internal/manager/certificates"
	"github.com/ameistad/haloy/internal/version"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

const (
	RefreshInterval  = 30 * time.Minute
	HAProxyConfigDir = "/haproxy-config"
	CertificatesDir  = "/cert-storage"
	HTTPProviderPort = "8080"
)

var logger = logrus.New()

type ContainerEvent struct {
	Event     events.Message
	Container container.InspectResponse
	Labels    *config.ContainerLabels
}

func RunManager(dryRun bool) {
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Enable debug logging if in dry run mode
	if dryRun {
		logger.SetLevel(logrus.DebugLevel)
		logger.Debug("Debug logging enabled")
	}

	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		logger.Fatalf("Failed to create Docker client: %v", err)
	}
	defer dockerClient.Close()

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Channel for Docker events
	eventsChan := make(chan ContainerEvent)
	errorsChan := make(chan error)

	// Channel for signaling cert updates needing HAProxy reload
	// Buffered channel to prevent blocking CertManager if RunManager is busy
	certUpdateSignal := make(chan string, 5)

	// Create deployment manager
	deploymentManager := NewDeploymentManager(dockerClient)

	// Create and start the certifications manager
	certManagerConfig := certificates.Config{
		CertDir:          CertificatesDir,
		HTTPProviderPort: HTTPProviderPort,
		Logger:           logger,
		TlsStaging:       dryRun,
	}
	certManager, err := certificates.NewManager(certManagerConfig, certUpdateSignal)
	if err != nil {
		logger.Fatalf("Failed to create certificate manager: %v", err)
		return
	}

	// Create the HAProxy manager
	haproxyManager := NewHAProxyManager(
		dockerClient,
		logger,
		HAProxyConfigDir,
		CertificatesDir,
		dryRun,
	)

	// Updater to glue deployment manager and certificate manager and handle HAProxy updates.
	updaterConfig := UpdaterConfig{
		DeploymentManager: deploymentManager,
		CertManager:       certManager,
		HAProxyManager:    haproxyManager,
		Logger:            logger,
	}
	updater := NewUpdater(updaterConfig)

	// Get the initial list of containers and build deployments
	if err := deploymentManager.BuildDeployments(ctx); err != nil {
		logger.Printf("Failed to build deployments: %v", err)
	}

	certDomains := deploymentManager.GetCertificateDomains()
	certManager.AddDomains(certDomains)
	certManager.Start()

	// Start Docker event listener
	go listenForDockerEvents(ctx, dockerClient, eventsChan, errorsChan)

	// Start periodic full refresh
	refreshTicker := time.NewTicker(RefreshInterval)
	defer refreshTicker.Stop()

	fmt.Printf("Haloy manager version %s started on network %s...\n", version.Version, config.DockerNetwork)

	// Main event loop
	for {
		select {
		case <-sigChan:
			fmt.Println("\nShutting down gracefully...")
			if certManager != nil {
				certManager.Stop()
			}
			cancel()
			return
		case e := <-eventsChan:
			reason := fmt.Sprintf("container %s: %s", e.Event.Action, e.Labels.AppName)
			logger.Printf("Received event: %s", reason)

			go func(event ContainerEvent, updateReason string) {
				deploymentCtx, cancelDeployment := context.WithCancel(ctx)
				defer cancelDeployment()

				u := updater
				if err := u.Update(deploymentCtx, updateReason); err != nil {
					u.logger.Errorf("Background update failed for %s: %v", updateReason, err)
				} else {
					u.logger.Infof("Background update completed for %s", updateReason)
				}
			}(e, reason)

		case domainUpdated := <-certUpdateSignal:
			reason := fmt.Sprintf("post-certificate update for %s", domainUpdated)
			logger.Infof("Received cert update signal: %s", reason)
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
					u.logger.Errorf("Background HAProxy update failed for %s: %v", updateReason, err)
				} else {
					u.logger.Infof("Background HAProxy update completed for %s", updateReason)
				}
			}(reason)

		case err := <-errorsChan:
			logger.Printf("Error from Docker events: %v", err)
		case <-refreshTicker.C:
			reason := "periodic full refresh"
			logger.Printf("Received event: %s", reason)

			go func(updateReason string) {
				deploymentCtx, cancelDeployment := context.WithCancel(ctx)
				defer cancelDeployment()

				u := updater // Capture updater
				if err := u.Update(deploymentCtx, updateReason); err != nil {
					u.logger.Errorf("Background update failed for %s: %v", updateReason, err)
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
					logger.Printf("Error inspecting container %s: %v", helpers.SafeIDPrefix(event.Actor.ID), err)
					continue
				}
				eligible := isContainerEligible(container)

				if eligible {
					labels, err := config.ParseContainerLabels(container.Config.Labels)
					if err != nil {
						logger.Printf("Error parsing container labels: %v", err)
						return
					}

					containerEvent := ContainerEvent{
						Event:     event,
						Container: container,
						Labels:    labels,
					}
					eventsChan <- containerEvent
					// TODO: remove this else block. It is only for testing.
				} else {
					logger.Printf("Container %s event but not eligible: %s", event.Action, helpers.SafeIDPrefix(event.Actor.ID))
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

// isContainerEligible checks if a container should be handled by haloy.
func isContainerEligible(container container.InspectResponse) bool {
	if container.Config.Labels["haloy.ignore"] == "true" {
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
