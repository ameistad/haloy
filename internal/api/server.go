package api

import (
	"net/http"
)

// Server holds dependencies for the API handlers.
type APIServer struct {
	router *http.ServeMux
	// ... other dependencies like a logger, config
}

func NewServer(apiToken string) *APIServer {
	s := &APIServer{
		router: http.NewServeMux(),
	}
	s.setupRoutes()
	return s
}

// ListenAndServe starts the HTTP server.
func (s *APIServer) ListenAndServe(addr string) error {
	return http.ListenAndServe(addr, s.router)
}
