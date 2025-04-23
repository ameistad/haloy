package logging

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
)

// ClientConfig defines the configuration options for creating a LogStreamClient.
// It controls connection parameters, filtering, and timeout behavior.
type ClientConfig struct {
	AppNameFilter string        // Filter for the application name (e.g., "myapp")
	UseDeadline   bool          // Whether to use read deadlines (true for deploy, false for logs cmd)
	ReadDeadline  time.Duration // Deadline duration if UseDeadline is true (e.g., 500ms)
	DialTimeout   time.Duration // Timeout for establishing the connection
	MinLevel      zerolog.Level // Minimum log level to display (e.g., zerolog.InfoLevel)
	Handler       LogHandlerFunc
}

// LogStreamClient connects to a log streaming server and filters log messages by application name.
// It supports different reading modes with or without deadlines depending on the use case.
type LogStreamClient struct {
	conn          net.Conn
	reader        *bufio.Reader
	appNameFilter string
	useDeadline   bool           // Flag to control read deadline usage. This will be true for deploy and false for logs cmd.
	readDeadline  time.Duration  // Duration for the read deadline if used
	minLevel      zerolog.Level  // Minimum log level to display
	handler       LogHandlerFunc // Function to handle log messages
}

// NewLogStreamClient creates and connects a new log stream client with retries.
func NewLogStreamClient(config ClientConfig) (*LogStreamClient, error) {

	if config.Handler == nil {
		return nil, errors.New("LogHandlerFunc must be provided in ClientConfig")
	}

	if config.DialTimeout == 0 {
		config.DialTimeout = 5 * time.Second // Default dial timeout
	}
	if config.UseDeadline && config.ReadDeadline == 0 {
		config.ReadDeadline = 500 * time.Millisecond // Default read deadline
	}

	var conn net.Conn
	var err error
	const maxRetries = 3
	const retryDelay = 250 * time.Millisecond

	for attempt := 1; attempt <= maxRetries; attempt++ {
		conn, err = net.DialTimeout("tcp", DefaultStreamAddress, config.DialTimeout)
		if err == nil {
			// Connection successful
			break
		}

		// Check if the error is a connection refused error
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			// Check for specific syscall errors indicating connection refused
			var sysErr *os.SyscallError
			if errors.As(opErr.Err, &sysErr) && sysErr.Err == syscall.ECONNREFUSED {
				// It's connection refused, retry if attempts remain
				if attempt < maxRetries {
					// log.Debug().Int("attempt", attempt).Msgf("Connection refused, retrying in %v...", retryDelay) // Optional: Add debug log
					time.Sleep(retryDelay)
					continue
				}
			}
		}

		// If it's not a connection refused error, or if retries are exhausted, return the error
		return nil, fmt.Errorf("failed to connect to log stream at %s after %d attempts: %w", DefaultStreamAddress, attempt, err)
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
		minLevel:      config.MinLevel,
		handler:       config.Handler,
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

// Stream reads log messages from the server connection, parses them, filters by level,
// and passes them to the configured handler. It detects deployment completion/failure
// messages and returns appropriate errors. The function runs until context cancellation
// or connection closure.
func (c *LogStreamClient) Stream(ctx context.Context) error { // Removed writer io.Writer
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
					// If using deadlines, timeout is expected, continue loop
					if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
						continue // Don't return error on timeout, just try reading again
					}
				}
				// EOF or closed connection is a clean exit
				if err == io.EOF || errors.Is(err, net.ErrClosed) {
					return nil // Clean exit
				}
				// Any other error is unexpected
				return fmt.Errorf("error reading from log stream: %w", err)
			}

			// Attempt to parse the line as JSON
			var logEntry map[string]any
			if json.Unmarshal(lineBytes, &logEntry) == nil {
				// JSON parsing successful, extract fields
				levelStr, _ := logEntry[LogFieldLevel].(string)
				messageStr, msgOk := logEntry[LogFieldMessage].(string)
				appName, appNameOk := logEntry[LogFieldAppName].(string)

				if !msgOk {
					// If message field is missing, pass raw line to handler
					c.handler(zerolog.NoLevel, string(lineBytes), appName)
					continue
				}

				// Parse the log level from the string
				parsedLevel, levelErr := zerolog.ParseLevel(levelStr)
				if levelErr != nil {
					parsedLevel = zerolog.NoLevel // Treat unknown levels as NoLevel
				}

				if parsedLevel >= c.minLevel {
					// Format the message potentially including error details
					finalMessage := messageStr
					if parsedLevel == zerolog.ErrorLevel || parsedLevel == zerolog.FatalLevel || parsedLevel == zerolog.PanicLevel {
						if errField, ok := logEntry[zerolog.ErrorFieldName].(string); ok && errField != "" {
							finalMessage = fmt.Sprintf("%s (error: %s)", messageStr, errField)
						}
					}
					// Pass level and formatted message to the handler
					// Add newline if the handler expects it (most terminal handlers will)
					c.handler(parsedLevel, finalMessage+"\n", appName)
				}

				// Check for completion status after handling the message
				status, statusOk := logEntry[LogFieldDeploymentStatus].(string)
				if statusOk && appNameOk && appName == c.appNameFilter {
					if status == LogDeploymentCompleted {
						return ErrManagerCompleted
					}
					if status == LogDeploymentFailed {
						errMsg := messageStr // Use the main message as default error
						if errStr, ok := logEntry[zerolog.ErrorFieldName].(string); ok && errStr != "" {
							errMsg = fmt.Sprintf("%s: %s", errMsg, errStr)
						}
						// Return the specific failure error with the message
						return fmt.Errorf("%w: %s", ErrManagerFailed, errMsg)
					}
				}

			} else {
				// JSON parsing failed, pass raw line to handler with NoLevel
				c.handler(zerolog.NoLevel, string(lineBytes), "") // Use handler instead of writer
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
