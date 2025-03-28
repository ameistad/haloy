package manager

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/manager/certificates"
	"github.com/ameistad/haloy/internal/version"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/sirupsen/logrus"
)

const (
	RefreshInterval  = 5 * time.Minute
	HAProxyConfigDir = "/haproxy-config"
	CertificatesDir  = "/cert-storage"
	HTTPProviderPort = "8080"
)

var logger = logrus.New()

type ContainerEvent struct {
	Event     events.Message
	Container container.InspectResponse
}

func RunManager(dryRun bool) {
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
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

	// Create deployment manager
	deploymentManager := NewDeploymentManager(dockerClient)

	// Create and start the certifications manager
	certManagerConfig := certificates.Config{
		CertDir:          CertificatesDir,
		HTTPProviderPort: HTTPProviderPort,
		Logger:           logger,
		TlsStaging:       dryRun,
	}
	certManager, err := certificates.NewManager(certManagerConfig)
	if err != nil {
		logger.Fatalf("Failed to create certificate manager: %v", err)
		return
	}
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
			switch e.Event.Action {
			case "start":
				log.Printf("Container %s event: %s", e.Event.Action, e.Event.Actor.ID[:12])

				labels, err := config.ParseContainerLabels(e.Container.Config.Labels)
				if err != nil {
					log.Printf("Error parsing container labels: %v", err)
					continue
				}

				log.Printf("Container %s has app name '%s' and deployment ID '%s'", e.Container.ID[:12], labels.AppName, labels.DeploymentID)

				// Execute in a goroutine to avoid blocking the event loop
				go func() {
					// Create a child context for the deployment process.
					deploymentCtx, cancelDeployment := context.WithCancel(ctx)
					defer cancelDeployment()

					log.Printf("Starting deployment for %s\n", labels.AppName)

					if err := deploymentManager.BuildDeployments(deploymentCtx); err != nil {
						log.Printf("Failed to build deployments: %v", err)
						return
					}

					if !deploymentManager.HasChanged() {
						log.Println("Deployment configuration unchanged, skipping HAProxy update")
						return
					}

					certDomains := deploymentManager.GetCertificateDomains()
					certManager.AddDomains(certDomains)
					certManager.Refresh()

					log.Printf("Generating HAProxy config with for %s\n", labels.AppName)
					deployments := deploymentManager.Deployments()
					buf, err := CreateHAProxyConfig(deployments)
					if err != nil {
						log.Printf("Failed to create config %v", err)
						return
					}

					configDirPath, err := config.ConfigDirPath()
					if err != nil {
						log.Printf("Failed to determine config directory path: %v", err)
						return
					}

					if !dryRun {
						if err := os.WriteFile(filepath.Join(HAProxyConfigDir, config.HAProxyConfigFileName), buf.Bytes(), 0644); err != nil {
							log.Printf("Failed to write updated config file: %v", err)
							return
						}
						log.Printf("Sending SIGUSR2 command to haproxy...")
						haproxyID, err := getHaproxyContainerID(deploymentCtx, dockerClient)
						if err != nil {
							log.Fatalf("Error locating HAProxy container: %v", err)
						}

						err = dockerClient.ContainerKill(deploymentCtx, haproxyID, "SIGUSR2")
						if err != nil {
							log.Printf("Failed to send SIGUSR2: %v", err)
						} else {
							log.Println("Sent SIGUSR2 to HAProxy")
						}
					} else {
						log.Printf("Generated HAProxy config would have been written to %s:\n%s", configDirPath, buf.String())
					}

					log.Printf("Deployment completed for app '%s' (deployment: '%s')",
						labels.AppName, labels.DeploymentID)
				}()

			case "die", "stop", "kill":
				log.Printf("Container %s event: %s", e.Event.Action, e.Event.Actor.ID[:12])

				labels, err := config.ParseContainerLabels(e.Container.Config.Labels)
				if err != nil {
					log.Printf("Error parsing container labels: %v", err)
					continue
				}

				// TODO: clean up old deployements:
				// - remove old containers
				// - remove domains from certManager
				// - create new deployments
				logger.Printf("Removing container %s", labels.AppName)

			}

		case err := <-errorsChan:
			log.Printf("Error from Docker events: %v", err)
		case <-refreshTicker.C:
			// Periodic full refresh
			log.Println("Performing periodic HAProxy configuration refresh")
			// Get all running containers on our network
			containers, err := dockerClient.ContainerList(ctx, container.ListOptions{})
			if err != nil {
				log.Printf("Error listing containers for refresh: %v", err)
				continue
			}

			for _, containerSummary := range containers {
				container, err := dockerClient.ContainerInspect(ctx, containerSummary.ID)
				if err != nil {
					continue
				}

				// Check if container is on our network
				eligible := isContainerEligible(container)
				if !eligible {
					continue
				}

				labels, err := config.ParseContainerLabels(container.Config.Labels)
				if err != nil {
					log.Printf("Error parsing container labels: %v", err)
					continue
				}

				// TODO: do the same as for the start event.
				logger.Printf("Refreshing container %s", labels.AppName)
			}

			log.Println("HAProxy configuration refresh completed")

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
					log.Printf("Error inspecting container %s: %v", event.Actor.ID[:12], err)
					continue
				}
				eligible := isContainerEligible(container)

				if eligible {
					containerEvent := ContainerEvent{
						Event:     event,
						Container: container,
					}
					eventsChan <- containerEvent
					// TODO: remove this else block. It is only for testing.
				} else {
					log.Printf("Container %s event but not eligible: %s", event.Action, event.Actor.ID[:12])
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

func getHaproxyContainerID(ctx context.Context, dockerClient *client.Client) (string, error) {
	inspect, err := dockerClient.ContainerInspect(ctx, "haloy-haproxy")
	if err != nil {
		return "", fmt.Errorf("failed to inspect container haloy-haproxy: %w", err)
	}
	return inspect.ID, nil
}
