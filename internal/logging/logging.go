package logging

import (
    "log/slog"
    "os"
)

// For backward compatibility during migration, you could keep some of the old constants
// and provide helper functions

// Legacy log levels for migration purposes
const (
    DEBUG = iota
    INFO
    SUCCESS
    WARN
    ERROR
    FATAL
)

// NewSlogLogger creates a new slog.Logger with optional streaming
func NewSlogLogger(level slog.Level, publisher StreamPublisher) *slog.Logger {
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

// Helper functions for common logging patterns
func LogWithDeployment(logger *slog.Logger, deploymentID string) *slog.Logger {
    return logger.With("deploymentID", deploymentID)
}

func LogDeploymentComplete(logger *slog.Logger, deploymentID string, message string) {
    logger.With("deploymentID", deploymentID, "complete", true).Info(message)
}
