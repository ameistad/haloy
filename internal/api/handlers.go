package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/version"
)

func (s *APIServer) handleRollback() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deploymentID := deploy.CreateDeploymentID()
		deploymentLogger := logging.NewDeploymentLogger(deploymentID, s.logLevel, s.logBroker)
	}
}

// handleDeploy returns an http.HandlerFunc for deploying an app.
func (s *APIServer) handleDeploy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deploymentID := deploy.CreateDeploymentID()

		// Create deployment-specific logger using the factory
		deploymentLogger := logging.NewDeploymentLogger(deploymentID, s.logLevel, s.logBroker)
		var req DeployRequest

		// Decode and validate the JSON request from the user
		if err := decodeJSON(r.Body, &req); err != nil {
			// If decoding fails, send a 400 Bad Request response.
			// http.Error is a simple way to send a plain text error.
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Normalize and validate the app configuration.
		normalizedAppConfig := req.AppConfig.Normalize()
		if err := normalizedAppConfig.Validate(); err != nil {
			log.Printf("app config not valid: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Start deployment in background
		go func() {
			ctx := context.Background()
			ctx, cancel := context.WithTimeout(ctx, deploy.DefaultContextTimeout)
			defer cancel()

			deploymentLogger.Info("Starting deployment", "app", normalizedAppConfig.Name)

			cli, err := docker.NewClient(ctx)
			if err != nil {
				deploymentLogger.Error("Failed to create Docker client", "error", err)
				return
			}
			defer cli.Close()

			if err := deploy.DeployApp(ctx, cli, deploymentID, *normalizedAppConfig, deploymentLogger); err != nil {
				deploymentLogger.Error("Deployment failed", "app", normalizedAppConfig.Name, "error", err)
				return
			}
			deploymentLogger.Info("Container deployment initiated", "app", normalizedAppConfig.Name)
		}()

		response := DeployResponse{
			DeploymentID: deploymentID,
			Message:      "Deployment initiated successfully.",
		}

		// Use our helper to write the JSON response.
		// We log any error from writeJSON as it's too late to send a different
		// response to the client once the headers have been written.
		if err := writeJSON(w, http.StatusAccepted, response); err != nil {
			log.Printf("Error writing JSON response: %v", err)
		}
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
	Service string `json:"service"`
}

// handleHealth returns a simple health check endpoint
func (s *APIServer) handleHealth() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response := HealthResponse{
			Status:  "ok",
			Service: "haloy-manager",
			Version: version.Version,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}
	}
}
