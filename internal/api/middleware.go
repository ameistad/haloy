package api

import (
	"log"
	"net/http"
)

func (s *APIServer) bearerTokenAuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Get the "Authorization: Bearer <token>" header
		// 2. Compare it to the server's configured secret token
		// 3. If invalid, write a 401 Unauthorized error and return.
		// 4. If valid, call the next handler in the chain:
		next.ServeHTTP(w, r)
	}
}

func (s *APIServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log the request method and URI.
		log.Printf("Received request: %s %s from %s", r.Method, r.RequestURI, r.RemoteAddr)

		// Call the next handler in the chain.
		next.ServeHTTP(w, r)
	})
}
