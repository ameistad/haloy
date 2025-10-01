package api

func (s *APIServer) setupRoutes() {
	authMiddleware := s.bearerTokenAuthMiddleware

	s.router.Handle("GET /health", s.handleHealth())
	s.router.Handle("POST /v1/deploy", authMiddleware(s.handleDeploy()))
	s.router.Handle("GET /v1/deploy/{deploymentID}/logs", authMiddleware(s.handleDeploymentLogs()))

	// Logs stream
	s.router.Handle("GET /v1/logs", authMiddleware(s.handleLogs()))

	// Rollback routes
	s.router.Handle("GET /v1/rollback/{appName}", authMiddleware(s.handleRollbackTargets()))
	s.router.Handle("POST /v1/rollback/{appName}", authMiddleware(s.handleRollback()))

	// Status routes
	s.router.Handle("GET /v1/status/{appName}", authMiddleware(s.handleAppStatus()))

	// Stop routes
	s.router.Handle("POST /v1/stop/{appName}", authMiddleware(s.handleStopApp()))

	// Version route
	s.router.Handle("GET /v1/version", s.handleVersion())
}
