package api

func (s *APIServer) setupRoutes() {
	authMiddleware := s.bearerTokenAuthMiddleware
	logMiddleware := s.loggingMiddleware

	s.router.Handle("GET /health", s.handleHealth())
	s.router.Handle("POST /v1/deploy", logMiddleware(authMiddleware(s.handleDeploy())))
	s.router.Handle("GET /v1/deploy/{deploymentID}/logs", authMiddleware(s.handleDeploymentLogs()))

	// Logs stream
	s.router.Handle("GET /v1/logs", authMiddleware(s.handleLogs()))

	// Rollback routes
	s.router.Handle("GET /v1/rollback/{appName}", authMiddleware(s.handleRollbackTargets()))
	s.router.Handle("POST /v1/rollback/{appName}/{targetDeploymentID}", authMiddleware(s.handleRollback()))

	// Secrets routes
	s.router.Handle("GET /v1/secrets", authMiddleware(s.handleSecretsList()))
	s.router.Handle("POST /v1/secrets", authMiddleware(s.handleSetSecret()))
	s.router.Handle("DELETE /v1/secrets/{name}", authMiddleware(s.handleDeleteSecret()))

	// Status routes
	s.router.Handle("GET /v1/status/{appName}", authMiddleware(s.handleAppStatus()))

	// Stop routes
	s.router.Handle("POST /v1/stop/{appName}", authMiddleware(s.handleStopApp()))
}
