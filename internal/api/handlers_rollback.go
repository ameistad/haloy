package api

import (
	"context"
	"log"
	"net/http"

	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/logging"
)

type RollbackResponse struct {
	DeploymentID string `json:"deploymentId"`
	Message      string `json:"message"`
}

func (s *APIServer) handleRollback() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.PathValue("appName")
		if appName == "" {
			http.Error(w, "App name is required", http.StatusBadRequest)
			return
		}
		targetDeploymentID := r.PathValue("deploymentID")
		if targetDeploymentID == "" {
			http.Error(w, "Target deployment ID is required", http.StatusBadRequest)
			return
		}
		newDeploymentID := deploy.CreateDeploymentID()
		deploymentLogger := logging.NewDeploymentLogger(newDeploymentID, s.logLevel, s.logBroker)

		// Start rollback in background
		go func() {
			ctx := r.Context()
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

		response := RollbackResponse{
			DeploymentID: newDeploymentID,
			Message:      "Rollback initiated successfully.",
		}
		if err := writeJSON(w, http.StatusAccepted, response); err != nil {
			log.Printf("Error writing JSON response: %v", err)
		}
	}

}

type RollbackTargetsResponse struct {
	Targets []deploy.RollbackTarget `json:"targets"`
}

func (s *APIServer) handleRollbackTargets() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.PathValue("appName")
		if appName == "" {
			http.Error(w, "App name is required", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		ctx, cancel := context.WithTimeout(ctx, deploy.DefaultContextTimeout)
		defer cancel()

		cli, err := docker.NewClient(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		targets, err := deploy.GetRollbackTargets(ctx, cli, appName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := RollbackTargetsResponse{
			Targets: targets,
		}

		writeJSON(w, http.StatusOK, response)
	}
}
