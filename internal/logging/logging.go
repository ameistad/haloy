package logging

import (
	"log/slog"
	"os"
)

// NewLogger creates a new slog.Logger with optional streaming
func NewLogger(level slog.Level, publisher StreamPublisher) *slog.Logger {
	// Create base handler (console output)
	opts := &slog.HandlerOptions{
		Level: level,
	}
	baseHandler := slog.NewTextHandler(os.Stdout, opts)

	// Wrap with stream handler if publisher provided
	if publisher != nil {
		handler := NewStreamHandler(publisher, baseHandler)
		return slog.New(handler)
	}

	return slog.New(baseHandler)
}

// LogFatal logs an error and exits the program
func LogFatal(logger *slog.Logger, message string, args ...any) {
	logger.Error(message, args...)
	os.Exit(1)
}

// LoggerFactory creates loggers for different purposes
type LoggerFactory interface {
	NewLogger(deploymentID string, level slog.Level) *slog.Logger
	NewDeploymentLogger(deploymentID string, level slog.Level) *slog.Logger
	Close()
}

// DefaultLoggerFactory implements LoggerFactory with streaming support
type DefaultLoggerFactory struct {
	streamPublisher StreamPublisher
}

// NewLoggerFactory creates a new logger factory with streaming support
func NewLoggerFactory(publisher StreamPublisher) LoggerFactory {
	return &DefaultLoggerFactory{
		streamPublisher: publisher,
	}
}

// NewLogger creates a logger with optional deployment ID for streaming
func (f *DefaultLoggerFactory) NewLogger(deploymentID string, level slog.Level) *slog.Logger {
	logger := NewLogger(level, f.streamPublisher)
	if deploymentID != "" {
		// Add deploymentID as a persistent attribute for streaming
		return logger.With("deploymentID", deploymentID)
	}
	return logger
}

// NewDeploymentLogger creates a logger specifically for deployment operations
func (f *DefaultLoggerFactory) NewDeploymentLogger(deploymentID string, level slog.Level) *slog.Logger {
	return f.NewLogger(deploymentID, level)
}

// Close closes the underlying stream publisher
func (f *DefaultLoggerFactory) Close() {
	if f.streamPublisher != nil {
		f.streamPublisher.Close()
	}
}

// LogDeploymentComplete marks a deployment as successfully completed
// This sends the completion signal that tells CLI clients to stop streaming
func LogDeploymentComplete(logger *slog.Logger, deploymentID, appName, message string) {
	logger.Info(message,
		"app", appName,
		"deploymentID", deploymentID,
		"complete", true,
	)
}

// LogDeploymentFailed marks a deployment as failed
// This sends the failure signal that tells CLI clients to stop streaming with error
func LogDeploymentFailed(logger *slog.Logger, deploymentID, appName, message string, err error) {
	logger.Error(message,
		"app", appName,
		"deploymentID", deploymentID,
		"error", err,
		"complete", true, // Also end stream on failure
		"failed", true,
	)
}
