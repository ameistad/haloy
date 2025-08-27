package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

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

		if err := decodeJSON(r.Body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Start deployment in background
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

			if err := deploy.DeployApp(ctx, cli, deploymentID, req.AppConfig, req.ConfigFormat, deploymentLogger); err != nil {
				logging.LogDeploymentFailed(deploymentLogger, deploymentID, req.AppConfig.Name, "Deployment failed", err)
				return
			}
		}()

		response := apitypes.DeployResponse{DeploymentID: deploymentID}

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

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Accel-Buffering", "no") // Nginx/HAProxy
		w.Header().Set("X-Buffering", "no")       // General proxy buffering
		w.Header().Set("Transfer-Encoding", "chunked")

		// Subscribe to logs for this deployment ID
		// Don't pass request context - use background context with manual cleanup
		logChan := s.logBroker.SubscribeDeployment(deploymentID)
		defer s.logBroker.UnsubscribeDeployment(deploymentID)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		// Send initial keepalive to establish connection
		if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
			return
		}
		flusher.Flush()

		// Handle incoming logs with keepalive
		ctx := r.Context()
		keepaliveTicker := time.NewTicker(30 * time.Second)
		defer keepaliveTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return

			case <-keepaliveTicker.C:
				// Send keepalive comment every 30 seconds
				if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
					return
				}
				flusher.Flush()

			case logEntry, ok := <-logChan:
				if !ok {
					return
				}

				if err := writeSSEMessage(w, logEntry); err != nil {
					return
				}
				flusher.Flush()

				// If deployment is complete or failed, end the stream
				if logEntry.IsDeploymentComplete || logEntry.IsDeploymentFailed {
					return
				}
			}
		}
	}
}

// writeSSEMessage writes a log entry as Server-Sent Event
func writeSSEMessage(w http.ResponseWriter, entry logging.LogEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal log entry: %w", err)
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	if err != nil {
		return fmt.Errorf("failed to write SSE data: %w", err)
	}

	return nil
}
