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

		ctx := r.Context()
		ctx, cancel := context.WithTimeout(ctx, defaultContextTimeout)
		defer cancel()

		cli, err := docker.NewClient(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer cli.Close()

		// Create a new logger without deployment ID because we'll stop all apps independent of their deployment id.
		logger := logging.NewLogger(s.logLevel, s.logBroker)

		stoppedIDs, err := docker.StopContainers(ctx, cli, logger, appName, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var removedIDs []string
		if removeContainers {
			removedIDs, err = docker.RemoveContainers(ctx, cli, logger, appName, "")
			if err != nil {
				// TODO: Log error but don't fail the request if containers were stopped
			}
		}

		response := apitypes.StopAppResponse{
			StoppedIDs: stoppedIDs,
			RemovedIDs: removedIDs,
		}

		writeJSON(w, http.StatusOK, response)
	}
}
