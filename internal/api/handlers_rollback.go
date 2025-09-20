package api

import (
	"context"
	"net/http"

	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/logging"
)

func (s *APIServer) handleRollback() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.PathValue("appName")
		if appName == "" {
			http.Error(w, "App name is required", http.StatusBadRequest)
			return
		}

		var req apitypes.RollbackRequest
		if err := decodeJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.TargetDeploymentID == "" {
			http.Error(w, "Target deployment ID is required", http.StatusBadRequest)
			return
		}
		if req.NewDeploymentID == "" {
			http.Error(w, "New deployment ID is required", http.StatusBadRequest)
			return
		}

		deploymentLogger := logging.NewDeploymentLogger(req.NewDeploymentID, s.logLevel, s.logBroker)

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

			if err := deploy.RollbackApp(ctx, cli, appName, req.TargetDeploymentID, req.NewDeploymentID, deploymentLogger); err != nil {
				deploymentLogger.Error("Deployment failed", "app", appName, "error", err)
				return
			}
			deploymentLogger.Info("Rollback initiated", "app", appName, "deploymentID", req.NewDeploymentID)
		}()

		w.WriteHeader(http.StatusAccepted)
	}
}

func (s *APIServer) handleRollbackTargets() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.PathValue("appName")
		if appName == "" {
			http.Error(w, "App name is required", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		ctx, cancel := context.WithTimeout(ctx, defaultContextTimeout)
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

		response := apitypes.RollbackTargetsResponse{
			Targets: targets,
		}

		writeJSON(w, http.StatusOK, response)
	}
}
