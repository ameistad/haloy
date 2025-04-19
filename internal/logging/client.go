package logging

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/ui"
	"github.com/rs/zerolog"
)

// LogStreamClient connects to a log streaming server and filters log messages by application name.
// It supports different reading modes with or without deadlines depending on the use case.
type LogStreamClient struct {
	conn          net.Conn
	reader        *bufio.Reader
	appNameFilter string
	useDeadline   bool          // Flag to control read deadline usage. This will be true for deploy and false for logs cmd.
	readDeadline  time.Duration // Duration for the read deadline if used
}

// ClientConfig defines the configuration options for creating a LogStreamClient.
// It controls connection parameters, filtering, and timeout behavior.
type ClientConfig struct {
	Address       string        // Address of the log stream server (e.g., "localhost:9000")
	AppNameFilter string        // Filter for the application name (e.g., "myapp")
	UseDeadline   bool          // Whether to use read deadlines (true for deploy, false for logs cmd)
	ReadDeadline  time.Duration // Deadline duration if UseDeadline is true (e.g., 500ms)
	DialTimeout   time.Duration // Timeout for establishing the connection
}

// NewLogStreamClient creates and connects a new log stream client.
func NewLogStreamClient(config ClientConfig) (*LogStreamClient, error) {
	if config.DialTimeout == 0 {
		config.DialTimeout = 5 * time.Second // Default dial timeout
	}
	if config.UseDeadline && config.ReadDeadline == 0 {
		config.ReadDeadline = 500 * time.Millisecond // Default read deadline
	}

	conn, err := net.DialTimeout("tcp", config.Address, config.DialTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to log stream at %s: %w", config.Address, err)
	}

	// Send the filter immediately after connecting
	filter := strings.TrimSpace(config.AppNameFilter)
	_, err = conn.Write([]byte(filter + "\n"))
	if err != nil {
		conn.Close() // Close connection if filter send fails
		return nil, fmt.Errorf("failed to send app name filter '%s' to log stream: %w", filter, err)
	}

	client := &LogStreamClient{
		conn:          conn,
		reader:        bufio.NewReader(conn),
		appNameFilter: filter,
		useDeadline:   config.UseDeadline,
		readDeadline:  config.ReadDeadline,
	}

	// Read and discard the welcome message from the server
	// Set a short deadline just for reading the welcome message
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = client.reader.ReadString('\n')
	conn.SetReadDeadline(time.Time{}) // Clear deadline
	if err != nil {
		// Don't fail if welcome message read fails, just log or ignore
		// fmt.Printf("Warning: could not read welcome message: %v\n", err)
	}

	return client, nil
}

// Stream reads log messages from the server connection and processes them according to
// their format (JSON or plain text). For JSON logs, it parses and formats them based on
// their log level. It detects deployment completion/failure messages and returns appropriate
// errors when these occur. The function runs until context cancellation or connection closure.
func (c *LogStreamClient) Stream(ctx context.Context, writer io.Writer) error { // writer might become unused if ui writes directly to stdout/stderr
	prefix := "[STREAM] " // Define prefix for streamed logs

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if c.useDeadline {
				c.conn.SetReadDeadline(time.Now().Add(c.readDeadline))
			}

			lineBytes, err := c.reader.ReadBytes('\n') // Read as bytes

			if c.useDeadline {
				c.conn.SetReadDeadline(time.Time{})
			}

			if err != nil {
				// Handle read errors (timeout, EOF, closed)
				if c.useDeadline {
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue
					}
				}
				if err == io.EOF || errors.Is(err, net.ErrClosed) {
					return nil // Clean exit
				}
				return fmt.Errorf("error reading from log stream: %w", err)
			}

			// Attempt to parse the line as JSON
			var logEntry map[string]interface{}
			if json.Unmarshal(lineBytes, &logEntry) == nil {
				// JSON parsing successful, extract fields
				levelStr, _ := logEntry[LogFieldLevel].(string)
				messageStr, msgOk := logEntry[LogFieldMessage].(string)

				if !msgOk {
					// If message field is missing, print raw line as fallback
					fmt.Fprint(writer, prefix+string(lineBytes)) // Use original writer
					continue
				}

				// Use ui package based on level
				switch levelStr {
				case zerolog.LevelInfoValue: // "info"
					ui.Info(prefix + messageStr + "\n")
				case zerolog.LevelErrorValue: // "error"
					// Optionally include error field if present
					errField, _ := logEntry[zerolog.ErrorFieldName].(string)
					if errField != "" {
						ui.Error(prefix+"%s (error: %s)\n", messageStr, errField)
					} else {
						ui.Error(prefix + messageStr + "\n")
					}
				case zerolog.LevelWarnValue: // "warn"
					ui.Warning(prefix + messageStr + "\n")
				case zerolog.LevelDebugValue: // "debug"
					// Decide if you want to show debug logs in the CLI stream
					// ui.Info(prefix+"[DEBUG] "+messageStr+"\n") // Example: show as Info
					// Or ignore them:
					// continue
					ui.Info(prefix + "[DEBUG] " + messageStr + "\n") // Show debug as info for now
				case zerolog.LevelFatalValue: // "fatal"
					ui.Error(prefix + "[FATAL] " + messageStr + "\n") // Show fatal as error
				case zerolog.LevelPanicValue: // "panic"
					ui.Error(prefix + "[PANIC] " + messageStr + "\n") // Show panic as error
				default:
					// Unknown level, print raw line
					fmt.Fprint(writer, prefix+string(lineBytes)) // Use original writer
				}

				// Check for completion status after printing the message
				appName, appOk := logEntry[LogFieldAppName].(string)
				status, statusOk := logEntry[LogFieldDeploymentStatus].(string)
				if statusOk && appOk && appName == c.appNameFilter {
					if status == LogDeploymentCompleted {
						return ErrManagerCompleted
					}
					if status == LogDeploymentFailed {
						errMsg := messageStr // Use the main message as default error
						if errStr, ok := logEntry[zerolog.ErrorFieldName].(string); ok {
							errMsg = fmt.Sprintf("%s: %s", errMsg, errStr)
						}
						return fmt.Errorf("%w: %s", ErrManagerFailed, errMsg)
					}
				}

			} else {
				// JSON parsing failed, print raw line
				fmt.Fprint(writer, prefix+string(lineBytes)) // Use original writer
			}
		}
	}
}

// Close closes the underlying network connection.
func (c *LogStreamClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

var (
	ErrManagerCompleted = errors.New("manager reported deployment completed")
	ErrManagerFailed    = errors.New("manager reported deployment failed")
)
