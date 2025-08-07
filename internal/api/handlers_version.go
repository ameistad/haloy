package api

import (
	"net/http"

	"github.com/ameistad/haloy/internal/apitypes"
	"github.com/ameistad/haloy/internal/constants"
)

func (s *APIServer) handleVersion() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		response := apitypes.VersionResponse{
			Version:        constants.Version,
			HAProxyVersion: constants.HAProxyVersion,
		}

		writeJSON(w, http.StatusOK, response)
	}
}
