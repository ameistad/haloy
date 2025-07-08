package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ameistad/haloy/internal/logging"
)

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

		// Subscribe to logs for this deployment ID
		// Don't pass request context - use background context with manual cleanup
		logChan := s.logBroker.Subscribe(deploymentID)
		defer s.logBroker.Unsubscribe(deploymentID) // Manual cleanup

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		// Send initial connection message
		// if err := writeSSEMessage(w, logging.LogEntry{
		// 	Level:        "INFO",
		// 	Message:      "Connected to deployment logs stream",
		// 	Timestamp:    time.Now(),
		// 	DeploymentID: deploymentID,
		// }); err != nil {
		// 	return
		// }
		// flusher.Flush()

		// Handle incoming logs
		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return

			case logEntry, ok := <-logChan:
				if !ok {
					return
				}

				if err := writeSSEMessage(w, logEntry); err != nil {
					return
				}
				flusher.Flush()

				// If deployment is complete or failed, end the stream
				if logEntry.IsComplete || logEntry.IsFailed {
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
