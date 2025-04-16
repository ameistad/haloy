package logging

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Define a private type for the context key to avoid collisions.
type contextKey string

const loggerKey = contextKey("logger")

// WithLogger returns a new context with the provided logger embedded.
func WithLogger(ctx context.Context, logger zerolog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// Ctx returns the logger embedded in the context, or the global logger if none is found.
func Ctx(ctx context.Context) *zerolog.Logger {
	if logger, ok := ctx.Value(loggerKey).(zerolog.Logger); ok {
		// Return a pointer to the logger found in context
		return &logger
	}
	// Fallback to the global logger
	// Note: zerolog/log.Ctx(ctx) does something similar but uses its own key.
	// We use our custom key to ensure we retrieve the logger *we* set.
	l := log.Logger // Get the global logger instance
	return &l
}

// GetLoggerFromContext retrieves the logger embedded in the context.
// It returns nil if no logger is found under the specific key.
func GetLoggerFromContext(ctx context.Context) *zerolog.Logger {
	if logger, ok := ctx.Value(loggerKey).(zerolog.Logger); ok {
		return &logger // Return pointer to the found logger
	}
	return nil // Indicate no logger was found in the context under our key
}
