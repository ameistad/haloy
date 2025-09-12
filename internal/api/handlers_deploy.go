package api

import (
	"context"
	"log"
	"net/http"

	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/logging"
)

// handleDeploy returns an http.HandlerFunc for deploying an app.
func (s *APIServer) handleDeploy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req apitypes.DeployRequest

		if err := decodeJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Create deployment-specific logger using the factory
		deploymentLogger := logging.NewDeploymentLogger(req.DeploymentID, s.logLevel, s.logBroker)

		go func() {
			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, defaultContextTimeout)
			defer cancel()

			cli, err := docker.NewClient(ctx)
			if err != nil {
				deploymentLogger.Error("Failed to create Docker client", "error", err)
				return
			}
			defer cli.Close()

			if err := deploy.DeployApp(ctx, cli, req.DeploymentID, req.AppConfig, req.ConfigFormat, deploymentLogger); err != nil {
				logging.LogDeploymentFailed(deploymentLogger, req.DeploymentID, req.AppConfig.Name, "Deployment failed", err)
				return
			}
		}()

		response := apitypes.DeployResponse{DeploymentID: req.DeploymentID}

		if err := writeJSON(w, http.StatusAccepted, response); err != nil {
			log.Printf("Error writing JSON response: %v", err)
		}
	}
}

// handleDeploymentLogs handles SSE connections for deployment logs
func (s *APIServer) handleDeploymentLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deploymentID := r.PathValue("deploymentID")
		if deploymentID == "" {
			http.Error(w, "deployment ID is required", http.StatusBadRequest)
			return
		}

		// Subscribe to logs for this deployment ID
		// Don't pass request context - use background context with manual cleanup
		logChan := s.logBroker.SubscribeDeployment(deploymentID)

		streamConfig := sseStreamConfig{
			logChan: logChan,
			cleanup: func() { s.logBroker.UnsubscribeDeployment(deploymentID) },
			shouldTerminate: func(logEntry logging.LogEntry) bool {
				return logEntry.IsDeploymentComplete || logEntry.IsDeploymentFailed
			},
		}

		streamSSELogs(w, r, streamConfig)
	}
}
