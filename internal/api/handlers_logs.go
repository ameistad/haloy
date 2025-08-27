package api

import (
	"net/http"
	"time"
)

func (s *APIServer) handleLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("X-Accel-Buffering", "no") // Nginx/HAProxy
		w.Header().Set("X-Buffering", "no")       // General proxy buffering
		w.Header().Set("Transfer-Encoding", "chunked")

		// Subscribe to general logs (all logs)
		logChan, subscriberID := s.logBroker.SubscribeGeneral()
		defer s.logBroker.UnsubscribeGeneral(subscriberID)

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
			}
		}
	}
}
