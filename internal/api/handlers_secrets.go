package api

import (
	"context"
	"log"
	"net/http"

	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/logging"
)

func (s *APIServer) handleSecretsList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.PathValue("appName")
		if appName == "" {
			http.Error(w, "App name is required", http.StatusBadRequest)
			return
		}
		targetDeploymentID := r.PathValue("targetDeploymentID")
		if targetDeploymentID == "" {
			http.Error(w, "Target deployment ID is required", http.StatusBadRequest)
			return
		}
		newDeploymentID := deploy.CreateDeploymentID()
		deploymentLogger := logging.NewDeploymentLogger(newDeploymentID, s.logLevel, s.logBroker)

		// Start rollback in background
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

			if err := deploy.RollbackApp(ctx, cli, appName, targetDeploymentID, newDeploymentID, deploymentLogger); err != nil {
				deploymentLogger.Error("Deployment failed", "app", appName, "error", err)
				return
			}
			deploymentLogger.Info("Rollback initiated", "app", appName, "deploymentID", newDeploymentID)
		}()

		response := RollbackResponse{DeploymentID: newDeploymentID}
		if err := writeJSON(w, http.StatusAccepted, response); err != nil {
			log.Printf("Error writing JSON response: %v", err)
		}
	}
}
