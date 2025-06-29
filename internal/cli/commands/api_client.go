package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ameistad/haloy/internal/config"
	"github.com/ameistad/haloy/internal/logging"
	"github.com/ameistad/haloy/internal/ui"
)

// APIClient handles communication with the haloy API
type APIClient struct {
	client   *http.Client
	baseURL  string
	apiToken string
}

// NewAPIClient creates a new API client for the given server URL
func NewAPIClient(serverURL string) *APIClient {
	// Load API token from environment
	token, err := config.LoadAPIToken()
	if err != nil {
		ui.Error("Failed to load API token: %v", err)
		ui.Info("Set HALOY_API_TOKEN environment variable or create a .env file")
		// Continue without token - let API calls fail with proper auth errors
	}
	return &APIClient{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL:  serverURL,
		apiToken: token,
	}
}

// setAuthHeader sets the Authorization header if we have a token
func (c *APIClient) setAuthHeader(req *http.Request) {
	if c.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}
}

// IsServerAvailable checks if the server is reachable
func (c *APIClient) IsServerAvailable(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}

	// Health endpoint doesn't require auth
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("server not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	return nil
}

type APIRequest struct {
	AppConfig *config.AppConfig `json:"app,omitempty"`
}

type APIResponse struct {
	DeploymentID string `json:"deploymentId,omitempty"`
	Message      string `json:"message"`
	Status       string `json:"status,omitempty"`
}

// ExecuteCommand sends a command to the API
func (c *APIClient) ExecuteCommand(ctx context.Context, command string, request APIRequest) (*APIResponse, error) {
	jsonData, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Debug: Log the request being sent
	ui.Debug("Sending request to %s: %s", command, string(jsonData))

	url := fmt.Sprintf("%s/v1/%s", c.baseURL, command)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeader(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("authentication failed - check your HALOY_API_TOKEN")
		}
		return nil, fmt.Errorf("%s request failed with status %d", command, resp.StatusCode)
	}

	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &apiResp, nil
}

// Convenience methods for specific commands
func (c *APIClient) Deploy(ctx context.Context, appConfig config.AppConfig) (*APIResponse, error) {
	request := APIRequest{
		AppConfig: &appConfig,
	}
	return c.ExecuteCommand(ctx, "deploy", request)
}

func (c *APIClient) Rollback(ctx context.Context, appConfig config.AppConfig) (*APIResponse, error) {
	request := APIRequest{
		AppConfig: &appConfig,
	}
	return c.ExecuteCommand(ctx, "rollback", request)
}

func (c *APIClient) Status(ctx context.Context, appConfig config.AppConfig) (*APIResponse, error) {
	request := APIRequest{
		AppConfig: &appConfig,
	}
	return c.ExecuteCommand(ctx, "status", request)
}

// LogStreamer handles streaming logs from the haloy API for any command
type LogStreamer struct {
	client   *http.Client
	baseURL  string
	apiToken string
}

// NewLogStreamer creates a new log streamer
func NewLogStreamer(serverURL string) *LogStreamer {
	token, err := config.LoadAPIToken()
	if err != nil {
		ui.Warn("Failed to load API token for log streaming: %v", err)
	}
	return &LogStreamer{
		baseURL:  serverURL,
		apiToken: token,
		client: &http.Client{
			Timeout: 0, // No timeout for streaming
		},
	}
}

// StreamLogs connects to the SSE endpoint and displays logs for any command
func (s *LogStreamer) StreamLogs(ctx context.Context, command, deploymentID string) error {
	url := fmt.Sprintf("%s/v1/%s/%s/logs", s.baseURL, command, deploymentID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create SSE request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if s.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiToken)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to log stream: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("authentication failed for log streaming - check your HALOY_API_TOKEN")
		}
		return fmt.Errorf("log stream returned status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		// Skip empty lines and SSE comment lines
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			var logEntry logging.LogEntry
			if err := json.Unmarshal([]byte(data), &logEntry); err != nil {
				ui.Warn("Failed to parse log entry: %v", err)
				continue
			}

			// Format and display the log entry
			s.displayLogEntry(logEntry)

			// Check if operation is complete
			if logEntry.IsComplete {
				return nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading log stream: %w", err)
	}

	return nil
}

// displayLogEntry formats and displays a log entry using the UI package
func (s *LogStreamer) displayLogEntry(entry logging.LogEntry) {
	// Debug: Show all fields for "Successfully deployed app" messages
	if strings.Contains(entry.Message, "Successfully deployed app") {
		fmt.Printf("DEBUG: Message='%s', Fields=%+v\n", entry.Message, entry.Fields)
	}
	timestamp := entry.Timestamp.Format("15:04:05")

	message := entry.Message
	// Handle the error field specially for multi-line errors
	if len(entry.Fields) > 0 {
		if errorValue, hasError := entry.Fields["error"]; hasError {

			// Convert error to string
			errorStr := fmt.Sprintf("%v", errorValue)

			// Check if it's a multi-line error (contains newlines)
			if strings.Contains(errorStr, "\n") {
				// For multi-line errors, display them after the main message
				switch strings.ToUpper(entry.Level) {
				case "ERROR":
					ui.Error("[%s] %s", timestamp, message)
					// Display the detailed error with proper indentation
					for _, line := range strings.Split(errorStr, "\n") {
						if strings.TrimSpace(line) != "" {
							ui.Error("    %s", line) // Indent error details
						}
					}
				case "WARN":
					ui.Warn("[%s] %s", timestamp, message)
					for _, line := range strings.Split(errorStr, "\n") {
						if strings.TrimSpace(line) != "" {
							ui.Warn("    %s", line)
						}
					}
				default:
					ui.Info("[%s] %s", timestamp, message)
					for _, line := range strings.Split(errorStr, "\n") {
						if strings.TrimSpace(line) != "" {
							fmt.Printf("    %s\n", line)
						}
					}
				}
				return // Early return since we handled the error specially
			} else {
				// Single-line error, append to message
				message = fmt.Sprintf("%s (error=%s)", message, errorStr)
			}
		}
	}
	switch strings.ToUpper(entry.Level) {
	case "ERROR":
		ui.Error("[%s] %s", timestamp, message)
	case "WARN":
		ui.Warn("[%s] %s", timestamp, message)
	case "INFO":
		if entry.IsSuccess {
			ui.Success("[%s] %s", timestamp, message)
		} else {
			ui.Info("[%s] %s", timestamp, message)
		}
	case "DEBUG":
		ui.Debug("[%s] %s", timestamp, message)
	default:
		fmt.Printf("[%s] %s\n", timestamp, message)
	}
}
