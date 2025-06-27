package api

func (s *APIServer) setupRoutes() {
	// A simple bearer token auth middleware
	authMiddleware := s.bearerTokenAuthMiddleware
	logMiddleware := s.loggingMiddleware

	// Health check endpoint (no auth required)
	s.router.Handle("GET /health", s.handleHealth())

	// Define handlers for each route
	s.router.Handle("POST /v1/deploy", logMiddleware(authMiddleware(s.handleDeploy())))
	s.router.Handle("GET /v1/deploy/{deploymentId}/logs", authMiddleware(s.handleDeploymentLogs()))
	// s.router.Handle("POST /v1/rollback/{appName}", authMiddleware(s.handleRollback()))
	// s.router.Handle("GET /v1/status/{appName}", authMiddleware(s.handleStatus()))
}
