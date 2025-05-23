package logging

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

type LogHandlerFunc func(level zerolog.Level, message string, appName string)

var (
	LogFieldLevel   = zerolog.LevelFieldName   // "level"
	LogFieldMessage = zerolog.MessageFieldName // "message"
)

const (
	LogFieldAppName          = "appName"
	LogFieldDeploymentStatus = "deployment_status"
	LogDeploymentCompleted   = "completed"
	LogDeploymentFailed      = "failed"
	DefaultStreamAddress     = ":9000"
)

// Init configures the global zerolog logger and optionally starts the log stream server.
// It returns the server instance (if created) so its lifecycle can be managed.
func Init(ctx context.Context, level zerolog.Level) (*Server, error) {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	zerolog.SetGlobalLevel(level)

	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
		NoColor:    false,
	}

	writers := []io.Writer{consoleWriter}
	var logServer *Server = nil

	logServer = NewServer(ctx, DefaultStreamAddress)
	if err := logServer.Listen(); err != nil {
		// Log using the basic console writer before the global one is fully set
		tempLogger := zerolog.New(consoleWriter).With().Timestamp().Logger()
		tempLogger.Error().Err(err).Msg("Log stream server failed to start during init")
		// Decide if this is fatal or if logging should continue without the stream
		// Returning the error might be best.
		return nil, fmt.Errorf("log server failed to start: %w", err)
	}

	streamWriter := &LogStreamWriter{Server: logServer, Context: ctx}
	writers = append(writers, streamWriter)

	multiWriter := zerolog.MultiLevelWriter(writers...)

	// Configure the global logger instance
	log.Logger = log.Output(multiWriter).With().Timestamp().Logger()

	log.Debug().Msg("Logger initialized")

	return logServer, nil
}
