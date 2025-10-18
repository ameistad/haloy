package api

import (
	"context"
	"net/http"

	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/docker"
	"github.com/ameistad/haloy/internal/logging"
)

func (s *APIServer) handleStopApp() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appName := r.PathValue("appName")
		if appName == "" {
			http.Error(w, "App name is required", http.StatusBadRequest)
			return
		}

		removeContainers := r.URL.Query().Get("remove-containers") == "true"

		logger := logging.NewLogger(s.logLevel, s.logBroker)

		go func() {
			ctx := context.Background()
			cli, err := docker.NewClient(ctx)
			if err != nil {
				logger.Error("Failed to create Docker client for stop operation", "app", appName, "error", err)
				return
			}
			defer cli.Close()

			logger.Info("Stopping containers", "app", appName)
			stoppedIDs, err := docker.StopContainers(ctx, cli, logger, appName, "")
			if err != nil {
				logger.Error("Failed to stop containers", "app", appName, "error", err)
				return
			}

			if removeContainers {
				logger.Info("Removing containers", "app", appName)
				removedIDs, err := docker.RemoveContainers(ctx, cli, logger, appName, "")
				if err != nil {
					logger.Error("Failed to remove containers", "app", appName, "error", err)
					return
				}
				logger.Info("Successfully removed containers", "app", appName, "removed_count", len(removedIDs), "container_ids", removedIDs)
			}

			logger.Info("Successfully stopped containers", "app", appName, "stopped_count", len(stoppedIDs), "container_ids", stoppedIDs)
		}()

		response := apitypes.StopAppResponse{
			Message: "Stop operation started. Use 'haloy logs' to monitor progress.",
		}

		if err := encodeJSON(w, http.StatusAccepted, response); err != nil {
			logger.Error("Failed to write response", "error", err)
		}
	}
}
