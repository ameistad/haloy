package manager

import (
	"context"
	"fmt"
	"io"
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
			switch e.Event.Action {
			case "start":
				logger.Printf("Container %s event: %s", e.Event.Action, e.Event.Actor.ID[:12])

				// Execute in a goroutine to avoid blocking the event loop

				// Create a child context for the deployment process.
				logger.Printf("Starting deployment for %s\n", e.Labels.AppName)
				deploymentCtx, cancelDeployment := context.WithCancel(ctx)
				defer cancelDeployment()
				if err := updateDeployment(updateDeploymentParams{
					Context:           deploymentCtx,
					DeploymentManager: deploymentManager,
					CertManager:       certManager,
					DockerClient:      dockerClient,
					DryRun:            dryRun,
					Reason:            fmt.Sprintf("container start: %s", e.Labels.AppName),
				}); err != nil {
					logger.Printf("Failed to update deployment: %v", err)
					return
				}

				logger.Printf("Deployment completed for app '%s' (deployment: '%s')",
					e.Labels.AppName, e.Labels.DeploymentID)

			case "die", "stop", "kill":
				logger.Printf("Container %s event: %s", e.Event.Action, e.Event.Actor.ID[:12])
				deploymentCtx, cancelDeployment := context.WithCancel(ctx)
				defer cancelDeployment()
				if err := updateDeployment(updateDeploymentParams{
					Context:           deploymentCtx,
					DeploymentManager: deploymentManager,
					CertManager:       certManager,
					DockerClient:      dockerClient,
					DryRun:            dryRun,
					Reason:            fmt.Sprintf("container start: %s", e.Labels.AppName),
				}); err != nil {
					logger.Printf("Failed to update deployment: %v", err)
					return
				}
				logger.Printf("Removing container %s", e.Labels.AppName)
			}

		case err := <-errorsChan:
			logger.Printf("Error from Docker events: %v", err)
		case <-refreshTicker.C:
			deploymentCtx, cancelDeployment := context.WithCancel(ctx)
			defer cancelDeployment()
			if err := updateDeployment(updateDeploymentParams{
				Context:           deploymentCtx,
				DeploymentManager: deploymentManager,
				CertManager:       certManager,
				DockerClient:      dockerClient,
				DryRun:            dryRun,
				Reason:            "periodic full refresh",
			}); err != nil {
				logger.Printf("Failed to update deployment: %v", err)
				return
			}
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
					logger.Printf("Error inspecting container %s: %v", event.Actor.ID[:12], err)
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
					logger.Printf("Container %s event but not eligible: %s", event.Action, event.Actor.ID[:12])
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

// updateDeploymentConfig handles the common flow of updating deployments and HAProxy configuration
type updateDeploymentParams struct {
	Context           context.Context
	DeploymentManager *DeploymentManager
	CertManager       *certificates.Manager
	DockerClient      *client.Client
	DryRun            bool
	Reason            string
}

func updateDeployment(params updateDeploymentParams) error {

	logger.Printf("Starting deployment update (%s)", params.Reason)

	if err := params.DeploymentManager.BuildDeployments(params.Context); err != nil {
		return fmt.Errorf("failed to build deployments: %w", err)
	}

	if !params.DeploymentManager.HasChanged() {
		return nil
	}

	// Update certificate domains
	certDomains := params.DeploymentManager.GetCertificateDomains()
	params.CertManager.AddDomains(certDomains)
	params.CertManager.Refresh()

	// Generate HAProxy configuration
	logger.Printf("Generating HAProxy config for %s", params.Reason)
	deployments := params.DeploymentManager.Deployments()
	buf, err := CreateHAProxyConfig(deployments)
	if err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}

	if !params.DryRun {
		// Write config and signal reload
		if err := os.WriteFile(filepath.Join(HAProxyConfigDir, config.HAProxyConfigFileName), buf.Bytes(), 0644); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}

		logger.Printf("Sending SIGUSR2 command to haproxy...")
		haproxyID, err := getHaproxyContainerID(params.Context, params.DockerClient)
		if err != nil {
			return fmt.Errorf("failed to get HAProxy container ID: %w", err)
		}

		err = params.DockerClient.ContainerKill(params.Context, haproxyID, "SIGUSR2")
		if err != nil {
			return fmt.Errorf("failed to send SIGUSR2 to HAProxy: %w", err)
		}
	} else {
		logger.Printf("Generated HAProxy config would have been written to %s:\n%s", HAProxyConfigDir, buf.String())
	}

	return nil
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
