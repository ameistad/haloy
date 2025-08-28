package api

import (
	"net/http"
)

func (s *APIServer) handleLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Subscribe to general logs (all logs)
		logChan, subscriberID := s.logBroker.SubscribeGeneral()

		streamConfig := sseStreamConfig{
			logChan: logChan,
			cleanup: func() { s.logBroker.UnsubscribeGeneral(subscriberID) },
		}

		streamSSELogs(w, r, streamConfig)
	}
}
