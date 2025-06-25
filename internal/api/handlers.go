package api

import (
	"context"
	"log"
	"net/http"

	"github.com/ameistad/haloy/internal/deploy"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/ui"
)

// handleDeploy returns an http.HandlerFunc for deploying an app.
func (s *APIServer) handleDeploy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req DeployRequest

		// 1. Decode and validate the JSON request from the user
		if err := decodeJSON(r.Body, &req); err != nil {
			// If decoding fails, send a 400 Bad Request response.
			// http.Error is a simple way to send a plain text error.
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Normalize and validate the app configuration.
		normalizedAppConfig := req.AppConfig.Normalize()
		if err := normalizedAppConfig.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), deploy.DefaultContextTimeout)
		defer cancel()
		cli, err := docker.NewClient(ctx)
		if err != nil {
			ui.Error("Failed to create Docker client: %v", err)
			return
		}
		defer cli.Close()

		if err := deploy.DeployApp(ctx, cli, req.AppConfig); err != nil {
			ui.Error("Failed to deploy %q: %v\n", req.AppConfig.Name, err)
		}

		// 2. Call the core updater logic.
		// (Assuming a placeholder s.updater.TriggerDeploy function for this example)
		// deploymentID, err := s.updater.TriggerDeploy(r.Context(), req.App)
		// if err != nil {
		// 	// For server-side errors, send a 500 Internal Server Error.
		// 	http.Error(w, "Failed to start deployment", http.StatusInternalServerError)
		// 	log.Printf("Error triggering deploy: %v", err) // Log the actual error
		// 	return
		// }

		// 3. Respond to the client
		response := DeployResponse{
			// DeploymentID: deploymentID,
			Message: "Deployment initiated successfully.",
		}

		// Use our helper to write the JSON response.
		// We log any error from writeJSON as it's too late to send a different
		// response to the client once the headers have been written.
		if err := writeJSON(w, http.StatusAccepted, response); err != nil {
			log.Printf("Error writing JSON response: %v", err)
		}
	}
}
