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
		deploymentID := deploy.CreateDeploymentID()

		// Create deployment-specific logger using the factory
		deploymentLogger := logging.NewDeploymentLogger(deploymentID, s.logLevel, s.logBroker)
		var req apitypes.DeployRequest

		// Decode and validate the JSON request from the user
		if err := decodeJSON(r.Body, &req); err != nil {
			// If decoding fails, send a 400 Bad Request response.
			// http.Error is a simple way to send a plain text error.
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Start deployment in background
		go func() {
			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, deploy.DefaultContextTimeout)
			defer cancel()

			cli, err := docker.NewClient(ctx)
			if err != nil {
				deploymentLogger.Error("Failed to create Docker client", "error", err)
				return
			}
			defer cli.Close()

			if err := deploy.DeployApp(ctx, cli, deploymentID, req.AppConfig, deploymentLogger); err != nil {
				deploymentLogger.Error("Deployment failed", "app", req.AppConfig.Name, "error", err)
				return
			}
		}()

		response := apitypes.DeployResponse{DeploymentID: deploymentID}

		if err := writeJSON(w, http.StatusAccepted, response); err != nil {
			log.Printf("Error writing JSON response: %v", err)
		}
	}
}
