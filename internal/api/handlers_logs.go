package api

import "net/http"

func (s *APIServer) handleLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		// Subscribe to general logs (all logs)
		logChan, subscriberID := s.logBroker.SubscribeGeneral()
		defer s.logBroker.UnsubscribeGeneral(subscriberID)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

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
			}
		}
	}
}
